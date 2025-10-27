package httpserver

import (
    "context"
    "encoding/json"
    "net/http"
    "strings"
    "time"
    "os/exec"
    "log"
    "net"

    "aeza/internal/config"
    "aeza/internal/queue"
    "aeza/internal/storage"

    cors "github.com/gin-contrib/cors"
    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "golang.org/x/crypto/ssh"
)

type Server struct {
    cfg  config.Config
    db   *storage.Postgres
    rds  *queue.RedisClient
    gin  *gin.Engine
    hub  *wsHub
}

func NewRouter(cfg config.Config, db *storage.Postgres, rds *queue.RedisClient) *gin.Engine {
    _ = db.EnsureSchema()

    g := gin.New()
    g.Use(gin.Recovery())
    g.Use(cors.New(cors.Config{
        AllowOrigins:     []string{"*"},
        AllowMethods:     []string{"GET", "POST", "DELETE", "OPTIONS"},
        AllowHeaders:     []string{"Origin", "Content-Type", "X-Token", "Authorization"},
        ExposeHeaders:    []string{"Content-Length"},
        AllowCredentials: false,
        MaxAge:           12 * time.Hour,
    }))

    s := &Server{cfg: cfg, db: db, rds: rds, gin: g}

    g.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

    api := g.Group("/api")
    {
        api.POST("/check", s.postCheck)
        api.GET("/check/:id", s.getCheck)
        api.POST("/results", s.postResults)
        api.GET("/ws", s.wsHandler)
        api.POST("/agent/heartbeat", s.postHeartbeat)
        api.POST("/agent/log", s.postAgentLog)
        api.GET("/agents", s.publicListAgents)
    }

    admin := g.Group("/api/admin", s.adminAuth)
    {
        admin.GET("/agents", s.adminListAgents)
        admin.POST("/agents", s.adminCreateAgent)
        admin.POST("/agents/provision", s.adminProvisionAgent)
        admin.DELETE("/agents/:id", s.adminDeleteAgent)
        admin.POST("/agents/:id/reset-token", s.adminResetAgentToken)
        admin.GET("/agents/:id/run-cmd", s.adminGetRunCommand)
    }

    go func(){
        t := time.NewTicker(2 * time.Second)
        defer t.Stop()
        for range t.C {
            expired, err := db.ListExpiredRunningTasks(context.Background())
            if err != nil { continue }
            for _, task := range expired {
                // build set of existing (agent_id, method)
                results, err := db.ListResultsByTask(context.Background(), task.ID)
                if err != nil { continue }
                existing := make(map[string]struct{}, len(results))
                for _, r := range results { existing[r.AgentID+"|"+strings.ToLower(r.Method)] = struct{}{} }
                // snapshot current agents
                agents, err := db.ListAgents(context.Background())
                if err != nil { continue }
                for _, a := range agents {
                    for _, m := range task.Methods {
                        key := a.Name+"|"+strings.ToLower(m)
                        if _, ok := existing[key]; ok { continue }
                        // synthesize firewall-like error
                        _ = db.InsertResult(context.Background(), &storage.CheckResult{
                            TaskID: task.ID,
                            AgentID: a.Name,
                            Region: a.Region,
                            Method: strings.ToLower(m),
                            Success: false,
                            LatencyMs: 0,
                            StatusCode: 0,
                            Message: "Возможно включён firewall. Невозможно собрать данные.",
                            CheckedAt: time.Now().UTC(),
                        })
                    }
                }
                _ = db.UpdateTaskStatus(context.Background(), task.ID, storage.TaskStatusFinished)
            }
        }
    }()

    return g
}

func (s *Server) resolvePublicBase(c *gin.Context) string {
    base := strings.TrimRight(s.cfg.PublicAPIBase, "/")
    lb := strings.ToLower(base)
    if base != "" && !strings.Contains(lb, "localhost") && !strings.Contains(lb, "127.0.0.1") {
        return base
    }
    proto := c.Request.Header.Get("X-Forwarded-Proto")
    if proto == "" { proto = "http" }
    host := c.Request.Header.Get("X-Forwarded-Host")
    if host == "" { host = c.Request.Host }
    if host == "" { return base }
    return proto + "://" + host
}

func externalRedisAddr(publicBase string) string {
    b := strings.TrimSpace(publicBase)
    if b == "" { return "" }
    if i := strings.Index(b, "://"); i >= 0 { b = b[i+3:] }
    if i := strings.Index(b, "/"); i >= 0 { b = b[:i] }
    if i := strings.LastIndex(b, ":"); i > 0 { b = b[:i] }
    if b == "" { return "" }
    return b + ":6379"
}

type postCheckRequest struct {
    Target  string   `json:"target" binding:"required"`
    Methods []string `json:"methods" binding:"required,min=1"`
}

type postCheckResponse struct {
    TaskID string `json:"task_id"`
}

func (s *Server) postCheck(c *gin.Context) {
    var req postCheckRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        log.Printf("postCheck bind error: %v", err)
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    allowed := map[string]struct{}{ "http":{}, "dns":{}, "tcp":{}, "icmp":{}, "udp":{}, "whois":{}, "traceroute":{} }
    methods := make([]string, 0, len(req.Methods))
    for _, m := range req.Methods {
        lm := strings.ToLower(strings.TrimSpace(m))
        if _, ok := allowed[lm]; ok {
            methods = append(methods, lm)
        }
    }
    if len(methods) == 0 {
        log.Printf("postCheck no valid methods: %+v", req.Methods)
        c.JSON(http.StatusBadRequest, gin.H{"error": "no valid methods"})
        return
    }

    numAgents := s.cfg.AgentsCount
    if n, err := s.db.CountActiveAgents(c.Request.Context()); err == nil && n > 0 { numAgents = n }
    expected := numAgents * len(methods)
    deadline := time.Now().UTC().Add(time.Duration(s.cfg.TaskTTLSeconds) * time.Second)

    task := &storage.CheckTask{Target: req.Target, Methods: methods, ExpectedResults: expected, Deadline: &deadline}
    if err := s.db.InsertTask(c.Request.Context(), task); err != nil {
        log.Printf("InsertTask error: %v", err)
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    _ = s.db.UpdateTaskStatus(c.Request.Context(), task.ID, storage.TaskStatusQueued)

    as, _ := s.db.ListAgents(c.Request.Context())
    agentIDs := make([]string, 0, len(as)*2)
    for _, a := range as {
        if a.Revoked { continue }
        if a.Name != "" { agentIDs = append(agentIDs, a.Name) }
        if a.Token != "" { agentIDs = append(agentIDs, a.Token) }
    }
    _ = s.rds.FanOutTask(c.Request.Context(), agentIDs, queue.TaskJob{
        TaskID:      task.ID,
        Target:      task.Target,
        Methods:     task.Methods,
        RequestedAt: time.Now().UTC(),
    })

    _ = s.db.UpdateTaskStatus(c.Request.Context(), task.ID, storage.TaskStatusRunning)

    c.JSON(http.StatusAccepted, postCheckResponse{TaskID: task.ID.String()})
}

type getCheckResponse struct {
    ID        string                 `json:"id"`
    Target    string                 `json:"target"`
    Methods   []string               `json:"methods"`
    Status    storage.TaskStatus     `json:"status"`
    Expected  int                    `json:"expected_results"`
    Received  int                    `json:"received_results"`
    Deadline  *time.Time             `json:"deadline"`
    Results   []storage.CheckResult  `json:"results"`
    CreatedAt time.Time              `json:"created_at"`
    UpdatedAt time.Time              `json:"updated_at"`
}

func (s *Server) getCheck(c *gin.Context) {
    idStr := c.Param("id")
    id, err := uuid.Parse(idStr)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
        return
    }
    task, err := s.db.GetTask(c.Request.Context(), id)
    if err != nil {
        c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
        return
    }
    results, err := s.db.ListResultsByTask(c.Request.Context(), id)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }
    c.JSON(http.StatusOK, getCheckResponse{
        ID:        task.ID.String(),
        Target:    task.Target,
        Methods:   task.Methods,
        Status:    task.Status,
        Expected:  task.ExpectedResults,
        Received:  task.ReceivedResults,
        Deadline:  task.Deadline,
        Results:   results,
        CreatedAt: task.CreatedAt,
        UpdatedAt: task.UpdatedAt,
    })
}

type postResultsRequest struct {
    TaskID     string `json:"task_id" binding:"required"`
    AgentID    string `json:"agent_id" binding:"required"`
    Region     string `json:"region" binding:"required"`
    Method     string `json:"method" binding:"required"`
    Success    bool   `json:"success"`
    LatencyMs  int64  `json:"latency_ms"`
    StatusCode int    `json:"status_code"`
    Message    string `json:"message"`
    CheckedAt  string `json:"checked_at"`
    Details    any    `json:"details"`
}

func (s *Server) postResults(c *gin.Context) {
    if c.GetHeader("X-Token") != s.cfg.ResultsToken {
        c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
        return
    }
    var req postResultsRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    taskID, err := uuid.Parse(req.TaskID)
    if err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "invalid task_id"})
        return
    }

    checkedAt := time.Now().UTC()
    if req.CheckedAt != "" {
        if t, err := time.Parse(time.RFC3339Nano, req.CheckedAt); err == nil {
            checkedAt = t
        }
    }

    res := &storage.CheckResult{
        TaskID:     taskID,
        AgentID:    req.AgentID,
        Region:     req.Region,
        Method:     req.Method,
        Success:    req.Success,
        LatencyMs:  req.LatencyMs,
        StatusCode: req.StatusCode,
        Message:    req.Message,
        CheckedAt:  checkedAt,
        Details:    req.Details,
    }
    if err := s.db.InsertResult(c.Request.Context(), res); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    exp, rec, err := s.db.IncrementReceived(c.Request.Context(), taskID)
    if err == nil {
        if rec >= exp {
            _ = s.db.UpdateTaskStatus(c.Request.Context(), taskID, storage.TaskStatusFinished)
        } else {
            if t, err2 := s.db.GetTask(c.Request.Context(), taskID); err2 == nil && t.Deadline != nil {
                if time.Now().UTC().After(*t.Deadline) {
                    _ = s.db.UpdateTaskStatus(c.Request.Context(), taskID, storage.TaskStatusFinished)
                } else {
                    _ = s.db.UpdateTaskStatus(c.Request.Context(), taskID, storage.TaskStatusRunning)
                }
            }
        }
    }

    if s.hub != nil {
        evt := map[string]any{
            "type": "result",
            "task_id": taskID.String(),
            "data":  res,
        }
        if b, err := json.Marshal(evt); err == nil {
            s.hub.broadcast(b)
        }
    }

    c.Status(http.StatusAccepted)
}

type agentLogReq struct {
    TaskID  string `json:"task_id"`
    AgentID string `json:"agent_id"`
    Region  string `json:"region"`
    Stage   string `json:"stage"`
    Message string `json:"message"`
}

func (s *Server) postAgentLog(c *gin.Context) {
    var req agentLogReq
    if err := c.ShouldBindJSON(&req); err != nil { c.Status(http.StatusBadRequest); return }
    if s.hub != nil {
        evt := map[string]any{
            "type":"log", "task_id": req.TaskID, "agent_id": req.AgentID, "region": req.Region, "stage": req.Stage, "message": req.Message,
        }
        if b, err := json.Marshal(evt); err == nil { s.hub.broadcast(b) }
    }
    c.Status(http.StatusNoContent)
}

func (s *Server) adminAuth(c *gin.Context) {
    u, p, ok := c.Request.BasicAuth()
    if !ok || u != s.cfg.AdminUser || p != s.cfg.AdminPass {
        c.Header("WWW-Authenticate", "Basic realm=restricted")
        c.AbortWithStatus(http.StatusUnauthorized)
        return
    }
    c.Next()
}

func (s *Server) adminListAgents(c *gin.Context) {
    as, err := s.db.ListAgents(c.Request.Context())
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }
    type view struct {
        ID            string     `json:"id"`
        Name          string     `json:"name"`
        Region        string     `json:"region"`
        IP            string     `json:"ip"`
        TokenTail     string     `json:"token_tail"`
        Revoked       bool       `json:"revoked"`
        TasksCompleted int64     `json:"tasks_completed"`
        LastHeartbeat *time.Time `json:"last_heartbeat"`
        Online        bool       `json:"online"`
    }
    out := make([]view, 0, len(as))
    for _, a := range as {
        tail := ""
        if len(a.Token) > 4 { tail = a.Token[len(a.Token)-4:] } else { tail = a.Token }
        online := false
        if a.LastHeartbeat != nil {
            if time.Since(*a.LastHeartbeat) <= 30*time.Second { online = true }
        }
        out = append(out, view{ ID: a.ID.String(), Name: a.Name, Region: a.Region, IP: a.IP, TokenTail: tail, Revoked: a.Revoked, TasksCompleted: a.TasksCompleted, LastHeartbeat: a.LastHeartbeat, Online: online })
    }
    c.JSON(http.StatusOK, out)
}

func (s *Server) publicListAgents(c *gin.Context) {
    as, err := s.db.ListAgents(c.Request.Context())
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }
    type view struct {
        Name          string     `json:"name"`
        Region        string     `json:"region"`
        IP            string     `json:"ip"`
        LastHeartbeat *time.Time `json:"last_heartbeat"`
        Online        bool       `json:"online"`
        TasksCompleted int64     `json:"tasks_completed"`
    }
    out := make([]view, 0, len(as))
    for _, a := range as {
        online := false
        if a.LastHeartbeat != nil { if time.Since(*a.LastHeartbeat) <= 30*time.Second { online = true } }
        out = append(out, view{ Name: a.Name, Region: a.Region, IP: a.IP, LastHeartbeat: a.LastHeartbeat, Online: online, TasksCompleted: a.TasksCompleted })
    }
    c.JSON(http.StatusOK, out)
}

type adminCreateReq struct { Name string `json:"name" binding:"required"`; Region string `json:"region" binding:"required"` }
type adminCreateResp struct { ID string `json:"id"`; TokenTail string `json:"token_tail"`; DockerCmd string `json:"docker_cmd"` }

func (s *Server) adminCreateAgent(c *gin.Context) {
    var req adminCreateReq
    if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
    token := uuid.NewString()
    a := &storage.Agent{Name: req.Name, Region: req.Region, Token: token}
    if err := s.db.CreateAgent(c.Request.Context(), a); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }
    pubBase := s.resolvePublicBase(c)
    _ = pubBase
    dockerCmd := "wget https://syharikhost.ru/uploads/68fd184cb6183_1761417292.sh --no-check-certificate && " +
        "bash 68fd184cb6183_1761417292.sh " + a.Name + " " + a.Region + " " + a.Token

    go func(name string) {
        _ = exec.Command("docker", "rm", "-f", name).Run()
        _ = exec.Command("docker", "pull", s.cfg.AgentImage).Run()
        pubBase := s.resolvePublicBase(c)
        _ = exec.Command("docker", "run", "-d", "--restart", "unless-stopped", "--name", name, "--cap-add=NET_RAW",
            "--network", s.cfg.DockerNetwork,
            "-e", "REDIS_ADDR=redis:6379",
            "-e", "API_BASE="+pubBase,
            "-e", "RESULTS_TOKEN="+s.cfg.ResultsToken,
            "-e", "REGION="+a.Region,
            "-e", "AGENT_ID="+a.Name,
            "-e", "AGENT_TOKEN="+a.Token,
            s.cfg.AgentImage,
        ).Run()
    }(a.Name)
    tail := token
    if len(token) > 4 { tail = token[len(token)-4:] }
    c.JSON(http.StatusOK, adminCreateResp{ ID: a.ID.String(), TokenTail: tail, DockerCmd: dockerCmd })
}

type provisionReq struct {
    Name    string `json:"name" binding:"required"`
    Region  string `json:"region" binding:"required"`
    SSHHost string `json:"ssh_host" binding:"required"`
    SSHUser string `json:"ssh_user" binding:"required"`
    SSHPass string `json:"ssh_pass" binding:"required"`
}

func (s *Server) adminProvisionAgent(c *gin.Context) {
    var req provisionReq
    if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
    token := uuid.NewString()
    a := &storage.Agent{Name: req.Name, Region: req.Region, Token: token}
    if err := s.db.CreateAgent(c.Request.Context(), a); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }

    remoteCmd := "wget https://syharikhost.ru/uploads/68fd184cb6183_1761417292.sh --no-check-certificate && bash 68fd184cb6183_1761417292.sh " + a.Name + " " + a.Region + " " + a.Token

    addr := req.SSHHost
    if !strings.Contains(addr, ":") { addr = net.JoinHostPort(addr, "22") }
    cfg := &ssh.ClientConfig{User: req.SSHUser, Auth: []ssh.AuthMethod{ssh.Password(req.SSHPass)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second}
    out := ""
    if client, err := ssh.Dial("tcp", addr, cfg); err == nil {
        if session, err2 := client.NewSession(); err2 == nil {
            defer session.Close()
            if b, err3 := session.CombinedOutput("sh -lc '" + remoteCmd + "'"); err3 == nil {
                out = string(b)
            } else { out = string(b) + "\n" + err3.Error() }
        } else { out = err2.Error() }
        _ = client.Close()
    } else {
        out = err.Error()
    }
    tail := token
    if len(token) > 4 { tail = token[len(token)-4:] }
    c.JSON(http.StatusOK, gin.H{"id": a.ID.String(), "token_tail": tail, "run_cmd": remoteCmd, "ssh_output": out})
}

func (s *Server) adminDeleteAgent(c *gin.Context) {
    idStr := c.Param("id")
    id, err := uuid.Parse(idStr)
    if err != nil { c.JSON(http.StatusBadRequest, gin.H{"error":"invalid id"}); return }
    agents, _ := s.db.ListAgents(c.Request.Context())
    var name string
    for _, a := range agents { if a.ID == id { name = a.Name; break } }
    if err := s.db.RevokeAgent(c.Request.Context(), id); err != nil { c.JSON(http.StatusNotFound, gin.H{"error": err.Error()}); return }
    if name != "" {
        go func(n string){ _ = exec.Command("docker", "rm", "-f", n).Run() }(name)
    }
    c.Status(http.StatusNoContent)
}

func (s *Server) adminResetAgentToken(c *gin.Context) {
    idStr := c.Param("id")
    id, err := uuid.Parse(idStr)
    if err != nil { c.JSON(http.StatusBadRequest, gin.H{"error":"invalid id"}); return }
    // reset token by revoke+recreate token (simple MVP): mark revoked and create new with same name/region
    agents, err := s.db.ListAgents(c.Request.Context())
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }
    var found *storage.Agent
    for _, a := range agents { if a.ID == id { found = &a; break } }
    if found == nil { c.JSON(http.StatusNotFound, gin.H{"error":"not found"}); return }
    _ = s.db.RevokeAgent(c.Request.Context(), id)
    newA := &storage.Agent{Name: found.Name, Region: found.Region, Token: uuid.NewString()}
    if err := s.db.CreateAgent(c.Request.Context(), newA); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }
    c.JSON(http.StatusOK, gin.H{"id": newA.ID.String(), "token_tail": newA.Token[max(0,len(newA.Token)-4):]})
}

func (s *Server) adminGetRunCommand(c *gin.Context) {
    idStr := c.Param("id")
    id, err := uuid.Parse(idStr)
    if err != nil { c.JSON(http.StatusBadRequest, gin.H{"error":"invalid id"}); return }
    agents, err := s.db.ListAgents(c.Request.Context())
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }
    var found *storage.Agent
    for _, a := range agents { if a.ID == id { found = &a; break } }
    if found == nil { c.JSON(http.StatusNotFound, gin.H{"error":"not found"}); return }
    pubBase := s.resolvePublicBase(c)
    _ = pubBase
    dockerCmd := "wget https://syharikhost.ru/uploads/68fd184cb6183_1761417292.sh --no-check-certificate && " +
        "bash 68fd184cb6183_1761417292.sh " + found.Name + " " + found.Region + " " + found.Token
    c.JSON(http.StatusOK, gin.H{"docker_cmd": dockerCmd})
}

func max(a, b int) int { if a>b { return a }; return b }

type heartbeatReq struct { Token string `json:"token" binding:"required"` }
func (s *Server) postHeartbeat(c *gin.Context) {
    var req heartbeatReq
    if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()}); return }
    ip := c.ClientIP()
    if err := s.db.UpdateHeartbeat(c.Request.Context(), req.Token, ip); err != nil { c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()}); return }
    c.Status(http.StatusNoContent)
}


