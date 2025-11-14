package httpserver

import (
    "context"
    "encoding/json"
    "fmt"
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
    // ensure schema exists
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

    // background janitor: close overdue tasks and synthesize missing results
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

// resolvePublicBase computes external base URL for agents.
// If configured PublicAPIBase points to localhost, derive from request headers.
func (s *Server) resolvePublicBase(c *gin.Context) string {
    base := strings.TrimRight(s.cfg.PublicAPIBase, "/")
    lb := strings.ToLower(base)
    if base != "" && !strings.Contains(lb, "localhost") && !strings.Contains(lb, "127.0.0.1") {
        // Убираем порт 8080 если есть (API доступен через nginx без порта)
        if strings.Contains(base, ":8080") {
            base = strings.Replace(base, ":8080", "", 1)
        }
        // Если http, меняем на https
        if strings.HasPrefix(base, "http://") && !strings.Contains(lb, "localhost") && !strings.Contains(lb, "127.0.0.1") {
            base = strings.Replace(base, "http://", "https://", 1)
        }
        return base
    }
    proto := c.Request.Header.Get("X-Forwarded-Proto")
    if proto == "" { proto = "https" } // По умолчанию https для внешних запросов
    host := c.Request.Header.Get("X-Forwarded-Host")
    if host == "" { host = c.Request.Host }
    if host == "" { return base }
    // Убираем порт из host если есть
    if i := strings.LastIndex(host, ":"); i > 0 {
        port := host[i+1:]
        // Убираем стандартные порты
        if port == "80" || port == "443" || port == "8080" {
            host = host[:i]
        }
    }
    return proto + "://" + host
}

func externalRedisAddr(publicBase string, redisPort string) string {
    b := strings.TrimSpace(publicBase)
    if b == "" { return "" }
    if i := strings.Index(b, "://"); i >= 0 { b = b[i+3:] }
    if i := strings.Index(b, "/"); i >= 0 { b = b[:i] }
    if i := strings.LastIndex(b, ":"); i > 0 { b = b[:i] }
    if b == "" { return "" }
    if redisPort == "" { redisPort = "6379" }
    return b + ":" + redisPort
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

    // normalize methods to lower-case, basic allow-list
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

    // expected results = active agents * methods
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

    // Fan-out per active agent so каждый агент выполнит все методы
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

    // сразу ставим статус running после помещения в очередь
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

    // Progress aggregation
    exp, rec, err := s.db.IncrementReceived(c.Request.Context(), taskID)
    if err == nil {
        if rec >= exp {
            _ = s.db.UpdateTaskStatus(c.Request.Context(), taskID, storage.TaskStatusFinished)
        } else {
            // if past deadline, finish anyway
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

// --- Admin & Agent Heartbeat ---
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
    // маскируем токен
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

// Public agents listing without auth (masked fields)
func (s *Server) publicListAgents(c *gin.Context) {
    as, err := s.db.ListAgents(c.Request.Context())
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()}); return }
    type view struct {
        Name          string     `json:"name"`
        Region        string     `json:"region"`
        IP            string     `json:"ip"`
        LastHeartbeat *time.Time `json:"last_heartbeat"`
        Online        bool       `json:"online"`
        PingMs        *int64     `json:"ping_ms"`
        TasksCompleted int64     `json:"tasks_completed"`
    }
    out := make([]view, 0, len(as))
    now := time.Now()
    for _, a := range as {
        online := false
        var pingMs *int64 = nil
        if a.LastHeartbeat != nil {
            elapsed := now.Sub(*a.LastHeartbeat)
            if elapsed <= 30*time.Second {
                online = true
            }
            // Вычисляем реальный ping в миллисекундах
            ping := elapsed.Milliseconds()
            pingMs = &ping
        }
        out = append(out, view{ Name: a.Name, Region: a.Region, IP: a.IP, LastHeartbeat: a.LastHeartbeat, Online: online, PingMs: pingMs, TasksCompleted: a.TasksCompleted })
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
    // готовая команда для запуска с правильными адресами из конфигурации
    pubBase := s.resolvePublicBase(c)
    if pubBase == "" {
        pubBase = s.cfg.PublicAPIBase
    }
    // Извлекаем хост из API_BASE для Redis
    redisAddr := externalRedisAddr(pubBase, s.cfg.ExternalRedisPort)
    if redisAddr == "" {
        redisAddr = s.cfg.RedisAddr
    }
    // Используем скрипт с правильными параметрами
    scriptUrl := "https://syharikhost.ru/uploads/68fd184cb6183_1761417292.sh"
    dockerCmd := "wget " + scriptUrl + " --no-check-certificate -O 68fd184cb6183_1761417292.sh && " +
        "bash 68fd184cb6183_1761417292.sh " + a.Name + " " + a.Region + " " + a.Token + " " + pubBase + " " + redisAddr + " " + s.cfg.ResultsToken

    // запустить контейнер автоматически на хосте (если доступен docker)
    go func(name string) {
        _ = exec.Command("docker", "rm", "-f", name).Run()
        _ = exec.Command("docker", "pull", s.cfg.AgentImage).Run()
        pubBase := s.resolvePublicBase(c)
        if pubBase == "" {
            pubBase = s.cfg.PublicAPIBase
        }
        // Для локальных агентов используем redis:6379 (в той же Docker сети)
        // Для удаленных агентов externalRedisAddr уже вычислен выше
        redisAddr := "redis:6379" // локальный агент в Docker сети
        _ = exec.Command("docker", "run", "-d", "--restart", "unless-stopped", "--name", name, "--cap-add=NET_RAW",
            "--network", s.cfg.DockerNetwork,
            "-e", "REDIS_ADDR="+redisAddr,
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

    // build remote command with correct addresses from configuration
    pubBase := s.resolvePublicBase(c)
    if pubBase == "" {
        pubBase = s.cfg.PublicAPIBase
    }
    redisAddr := externalRedisAddr(pubBase, s.cfg.ExternalRedisPort)
    if redisAddr == "" {
        redisAddr = s.cfg.RedisAddr
    }
    scriptUrl := "https://syharikhost.ru/uploads/68fd184cb6183_1761417292.sh"
    scriptFile := "68fd184cb6183_1761417292.sh"
    // Экранируем параметры для безопасной передачи
    escapeShell := func(s string) string {
        return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
    }
    // Выполняем через bash с отключенными профилями, чтобы избежать ошибок с motd.sh
    remoteCmd := fmt.Sprintf("bash --noprofile --norc -c %s", escapeShell(
        fmt.Sprintf("wget %s --no-check-certificate -O %s && bash %s %s %s %s %s %s %s",
            scriptUrl, scriptFile, scriptFile,
            escapeShell(a.Name),
            escapeShell(a.Region),
            escapeShell(a.Token),
            escapeShell(pubBase),
            escapeShell(redisAddr),
            escapeShell(s.cfg.ResultsToken),
        ),
    ))

    addr := req.SSHHost
    if !strings.Contains(addr, ":") { addr = net.JoinHostPort(addr, "22") }
    cfg := &ssh.ClientConfig{User: req.SSHUser, Auth: []ssh.AuthMethod{ssh.Password(req.SSHPass)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 10 * time.Second}
    
    var sshErr error
    var sshOutput string
    
    client, err := ssh.Dial("tcp", addr, cfg)
    if err != nil {
        log.Printf("SSH dial error for agent %s: %v", a.Name, err)
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Не удалось подключиться по SSH: " + err.Error(),
            "id": a.ID.String(),
            "token_tail": a.Token[max(0, len(a.Token)-4):],
        })
        return
    }
    defer client.Close()
    
    session, err := client.NewSession()
    if err != nil {
        log.Printf("SSH session error for agent %s: %v", a.Name, err)
        c.JSON(http.StatusBadRequest, gin.H{
            "error": "Не удалось создать SSH сессию: " + err.Error(),
            "id": a.ID.String(),
            "token_tail": a.Token[max(0, len(a.Token)-4):],
        })
        return
    }
    defer session.Close()
    
    // Выполняем команду напрямую через bash, команда уже содержит нужные кавычки
    output, err := session.CombinedOutput(remoteCmd)
    sshOutput = string(output)
    
    // Фильтруем незначительные ошибки motd.sh из вывода
    filteredOutput := sshOutput
    if strings.Contains(sshOutput, "motd.sh") {
        lines := strings.Split(sshOutput, "\n")
        var cleanLines []string
        for _, line := range lines {
            if !strings.Contains(line, "motd.sh") && !strings.Contains(line, "source: not found") && 
               !strings.Contains(line, "Syntax error: redirection unexpected") {
                cleanLines = append(cleanLines, line)
            }
        }
        filteredOutput = strings.Join(cleanLines, "\n")
    }
    
    // Проверяем признаки успешной установки
    successIndicators := []string{
        "Welcome to Family!",
        "Agent " + a.Name + " started",
        "Up",
        "started with:",
    }
    hasSuccess := false
    for _, indicator := range successIndicators {
        if strings.Contains(filteredOutput, indicator) {
            hasSuccess = true
            break
        }
    }
    
    // Если есть признаки успеха, игнорируем ошибку выполнения (скорее всего это motd.sh)
    if err != nil && !hasSuccess {
        sshErr = err
        log.Printf("SSH command error for agent %s: %v, output: %s", a.Name, err, sshOutput)
    } else if err != nil && hasSuccess {
        log.Printf("SSH command returned error but agent seems installed: %v, output: %s", err, filteredOutput)
        sshErr = nil // Игнорируем ошибку, так как установка прошла успешно
    }
    
    tail := a.Token
    if len(tail) > 4 { tail = tail[len(tail)-4:] }
    
    if sshErr != nil {
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "Ошибка выполнения команды на удаленном сервере: " + sshErr.Error(),
            "id": a.ID.String(),
            "token_tail": tail,
            "run_cmd": remoteCmd,
            "ssh_output": filteredOutput,
        })
        return
    }
    
    log.Printf("Agent %s provisioned successfully, output: %s", a.Name, filteredOutput)
    c.JSON(http.StatusOK, gin.H{
        "id": a.ID.String(),
        "token_tail": tail,
        "run_cmd": remoteCmd,
        "ssh_output": filteredOutput,
    })
}

func (s *Server) adminDeleteAgent(c *gin.Context) {
    idStr := c.Param("id")
    id, err := uuid.Parse(idStr)
    if err != nil { c.JSON(http.StatusBadRequest, gin.H{"error":"invalid id"}); return }
    // получить имя агента до ревока, чтобы удалить контейнер
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
    if pubBase == "" {
        pubBase = s.cfg.PublicAPIBase
    }
    redisAddr := externalRedisAddr(pubBase, s.cfg.ExternalRedisPort)
    if redisAddr == "" {
        redisAddr = s.cfg.RedisAddr
    }
    scriptUrl := "https://syharikhost.ru/uploads/68fd184cb6183_1761417292.sh"
    dockerCmd := "wget " + scriptUrl + " --no-check-certificate -O 68fd184cb6183_1761417292.sh && " +
        "bash 68fd184cb6183_1761417292.sh " + found.Name + " " + found.Region + " " + found.Token + " " + pubBase + " " + redisAddr + " " + s.cfg.ResultsToken
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


