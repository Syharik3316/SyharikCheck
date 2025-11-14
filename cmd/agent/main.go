package main

import (
    "context"
    "crypto/tls"
    "encoding/json"
    "fmt"
    "log"
    "net"
    "net/http"
    "net/url"
    "os/exec"
    "os"
    "strings"
    "time"

    "aeza/internal/queue"

    "github.com/google/uuid"
    "github.com/redis/go-redis/v9"
)
// best-effort GeoIP via ipapi.co (no key, rate-limited). Returns map or nil on error.
func geoIPLookup(host string) map[string]any {
    // If host is a URL, extract hostname
    h := hostnameForDNS(host)
    // Try to resolve to IP if domain
    ip := h
    if net.ParseIP(h) == nil {
        if ips, err := net.LookupIP(h); err == nil && len(ips) > 0 {
            ip = ips[0].String()
        }
    }
    client := &http.Client{Timeout: 5 * time.Second}
    req, _ := http.NewRequest(http.MethodGet, "https://ipapi.co/"+ip+"/json/", nil)
    resp, err := client.Do(req)
    if err != nil { return nil }
    defer resp.Body.Close()
    var m map[string]any
    if err := json.NewDecoder(resp.Body).Decode(&m); err != nil { return nil }
    return m
}

type AgentConfig struct {
    RedisAddr     string
    RedisPassword string
    RedisDB       int
    APIBaseURL    string
    ResultsToken  string
    AgentID       string
    Region        string
    AgentToken    string
}

func getenv(k, d string) string { if v := os.Getenv(k); v != "" { return v }; return d }

func loadConfig() AgentConfig {
    return AgentConfig{
        RedisAddr:     getenv("REDIS_ADDR", "redis:6379"),
        RedisPassword: getenv("REDIS_PASSWORD", ""),
        APIBaseURL:    strings.TrimRight(getenv("API_BASE", "http://api:8080"), "/"),
        ResultsToken:  getenv("RESULTS_TOKEN", "dev-token"),
        AgentID:       getenv("AGENT_ID", uuid.NewString()),
        Region:        getenv("REGION", "unknown"),
        AgentToken:    getenv("AGENT_TOKEN", ""),
    }
}

func ensureHTTPURL(target string) string {
    t := strings.TrimSpace(target)
    if strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://") {
        return t
    }
    // if looks like host:port, prepend http://
    return "http://" + t
}

func httpCheck(target string) (ok bool, code int, latency int64, msg string, headers map[string][]string) {
    t := ensureHTTPURL(target)
    client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
    start := time.Now()
    resp, err := client.Get(t)
    if err != nil { return false, 0, time.Since(start).Milliseconds(), err.Error(), nil }
    defer resp.Body.Close()
    return resp.StatusCode < 500, resp.StatusCode, time.Since(start).Milliseconds(), "", resp.Header
}

func hostnameForDNS(target string) string {
    t := strings.TrimSpace(target)
    if strings.Contains(t, "://") {
        if u, err := url.Parse(t); err == nil {
            return u.Hostname()
        }
    }
    // strip path if accidentally present
    if i := strings.Index(t, "/"); i > 0 {
        t = t[:i]
    }
    // strip port if present
    if h, _, err := net.SplitHostPort(t); err == nil {
        return h
    }
    return t
}

func dnsCheck(target string) (ok bool, latency int64, msg string, details map[string]any) {
    start := time.Now()
    host := hostnameForDNS(target)
    details = map[string]any{}
    // A
    if addrs, err := net.LookupHost(host); err == nil { details["A"] = addrs }
    // AAAA
    if ips, err := net.LookupIP(host); err == nil {
        var v6 []string
        for _, ip := range ips { if ip.To4() == nil { v6 = append(v6, ip.String()) } }
        if len(v6) > 0 { details["AAAA"] = v6 }
    }
    // MX
    if mx, err := net.LookupMX(host); err == nil {
        out := make([]string, 0, len(mx))
        for _, r := range mx { out = append(out, fmt.Sprintf("%s %d", strings.TrimSuffix(r.Host, "."), r.Pref)) }
        if len(out) > 0 { details["MX"] = out }
    }
    // NS
    if ns, err := net.LookupNS(host); err == nil {
        out := make([]string, 0, len(ns))
        for _, r := range ns { out = append(out, strings.TrimSuffix(r.Host, ".")) }
        if len(out) > 0 { details["NS"] = out }
    }
    // TXT
    if txt, err := net.LookupTXT(host); err == nil && len(txt) > 0 { details["TXT"] = txt }
    return true, time.Since(start).Milliseconds(), "", details
}

func tcpAddress(target string) string {
    t := strings.TrimSpace(target)
    if strings.Contains(t, "://") {
        if u, err := url.Parse(t); err == nil {
            host := u.Hostname()
            port := u.Port()
            if port == "" {
                if u.Scheme == "https" { port = "443" } else { port = "80" }
            }
            return net.JoinHostPort(host, port)
        }
    }
    // if path present, strip after '/'
    if i := strings.Index(t, "/"); i > 0 { t = t[:i] }
    // if no port, default 80
    if _, _, err := net.SplitHostPort(t); err != nil {
        return net.JoinHostPort(t, "80")
    }
    return t
}

func tcpCheck(target string) (ok bool, latency int64, msg string) {
    start := time.Now()
    addr := tcpAddress(target)
    conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
    if err != nil { return false, time.Since(start).Milliseconds(), err.Error() }
    _ = conn.Close()
    return true, time.Since(start).Milliseconds(), ""
}

// ICMP ping via external binary (portable MVP). Requires CAP_NET_RAW in container.
func icmpCheck(target string) (ok bool, latency int64, msg string) {
    host := hostnameForDNS(target)
    start := time.Now()
    // macOS uses different ping flags; use 1 echo universally: -c 1, and timeout 5s
    // BusyBox ping uses -W as seconds for timeout; iputils uses ms. Для кросс-платформенности используем 5s.
    cmd := exec.Command("ping", "-c", "1", "-W", "5", host)
    if out, err := cmd.CombinedOutput(); err != nil {
        return false, time.Since(start).Milliseconds(), string(out)
    }
    return true, time.Since(start).Milliseconds(), ""
}

// UDP check: try to Dial UDP and write empty packet (best-effort)
func udpCheck(target string) (ok bool, latency int64, msg string) {
    addr := tcpAddress(target) // reuse host:port normalization
    start := time.Now()
    udpAddr, err := net.ResolveUDPAddr("udp", addr)
    if err != nil { return false, 0, err.Error() }
    conn, err := net.DialUDP("udp", nil, udpAddr)
    if err != nil { return false, time.Since(start).Milliseconds(), err.Error() }
    defer conn.Close()
    conn.SetWriteDeadline(time.Now().Add(2*time.Second))
    if _, err := conn.Write([]byte{}); err != nil {
        return false, time.Since(start).Milliseconds(), err.Error()
    }
    return true, time.Since(start).Milliseconds(), ""
}

// WHOIS query using TCP port 43 (basic)
func whoisCheck(target string) (ok bool, latency int64, msg string) {
    host := hostnameForDNS(target)
    start := time.Now()
    conn, err := net.DialTimeout("tcp", net.JoinHostPort("whois.iana.org", "43"), 5*time.Second)
    if err != nil { return false, time.Since(start).Milliseconds(), err.Error() }
    defer conn.Close()
    _ = conn.SetDeadline(time.Now().Add(5*time.Second))
    if _, err := conn.Write([]byte(host + "\r\n")); err != nil {
        return false, time.Since(start).Milliseconds(), err.Error()
    }
    buf := make([]byte, 256)
    if _, err := conn.Read(buf); err != nil {
        // even если не прочитали — сам факт коннекта уже успех
        return true, time.Since(start).Milliseconds(), "partial read"
    }
    return true, time.Since(start).Milliseconds(), ""
}

// Traceroute using system traceroute (best-effort)
func traceroute(target string) (ok bool, latency int64, msg string, hops []map[string]any) {
    host := hostnameForDNS(target)
    start := time.Now()
    // BusyBox: traceroute -m 20 -w 2 host
    cmd := exec.Command("traceroute", "-m", "20", "-w", "2", host)
    out, err := cmd.CombinedOutput()
    if err != nil { return false, time.Since(start).Milliseconds(), string(out), nil }
    lines := strings.Split(string(out), "\n")
    for _, ln := range lines {
        line := strings.TrimSpace(ln)
        if line == "" { continue }
        // expected: "1 hostname (ip) rtt ms rtt ms rtt ms" or with stars
        fields := strings.Fields(line)
        if len(fields) == 0 { continue }
        hopNum := fields[0]
        var hostPart, ipPart string
        // find (ip)
        if i := strings.Index(line, "("); i >= 0 {
            if j := strings.Index(line[i:], ")"); j > 0 {
                ipPart = strings.TrimSpace(line[i+1 : i+j])
                hostPart = strings.TrimSpace(strings.TrimSpace(line[len(hopNum):i]))
            }
        }
        if hostPart == "" && len(fields) > 1 { hostPart = fields[1] }
        rtts := []string{}
        for _, f := range fields {
            if strings.HasSuffix(f, "ms") {
                rtts = append(rtts, strings.TrimSuffix(f, "ms"))
            }
        }
        hops = append(hops, map[string]any{"hop": hopNum, "host": strings.TrimSpace(hostPart), "ip": strings.Trim(ipPart, ")("), "rtt_ms": rtts})
    }
    return true, time.Since(start).Milliseconds(), "", hops
}

func postResult(ctx context.Context, cfg AgentConfig, r map[string]any) error {
    b, _ := json.Marshal(r)
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIBaseURL+"/api/results", strings.NewReader(string(b)))
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-Token", cfg.ResultsToken)
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode >= 300 { return fmt.Errorf("bad status: %d", resp.StatusCode) }
    return nil
}

func sendLog(ctx context.Context, cfg AgentConfig, taskID, stage, message string) {
    body := map[string]any{"task_id": taskID, "agent_id": cfg.AgentID, "region": cfg.Region, "stage": stage, "message": message}
    b, _ := json.Marshal(body)
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIBaseURL+"/api/agent/log", strings.NewReader(string(b)))
    req.Header.Set("Content-Type", "application/json")
    client := &http.Client{Timeout: 3 * time.Second}
    _, _ = client.Do(req)
}

func sendHeartbeat(ctx context.Context, cfg AgentConfig) {
    payload := map[string]any{"token": cfg.AgentToken}
    if cfg.AgentToken == "" { payload["token"] = cfg.AgentID }
    b, _ := json.Marshal(payload)
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIBaseURL+"/api/agent/heartbeat", strings.NewReader(string(b)))
    req.Header.Set("Content-Type", "application/json")
    client := &http.Client{Timeout: 5 * time.Second}
    _, _ = client.Do(req)
}

func main() {
    cfg := loadConfig()
    rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
    ctx := context.Background()

    // heartbeat loop
    go func(){
        t := time.NewTicker(15 * time.Second)
        defer t.Stop()
        for {
            select {
            case <-t.C:
                sendHeartbeat(ctx, cfg)
            }
        }
    }()

    // consume per-agent queue if present, else fall back to shared queue
    queueKey := "check_tasks:" + cfg.AgentID
    for {
        res, err := rdb.BRPop(ctx, 0, queueKey, "check_tasks").Result()
        if err != nil { log.Printf("BRPOP error: %v", err); time.Sleep(1*time.Second); continue }
        if len(res) != 2 { continue }
        var job queue.TaskJob
        if err := json.Unmarshal([]byte(res[1]), &job); err != nil { log.Printf("bad job: %v", err); continue }

        // последовательное выполнение методов с логами
        sendLog(ctx, cfg, job.TaskID.String(), "start", fmt.Sprintf("Начало проверки: %v", job.Methods))
        for _, m0 := range job.Methods {
            m := strings.ToLower(m0)
            sendLog(ctx, cfg, job.TaskID.String(), m, "Старт метода")
            switch m {
                case "http":
                    ok, code, lat, msg, hdrs := httpCheck(job.Target)
                    _ = postResult(ctx, cfg, map[string]any{
                        "task_id": job.TaskID.String(),
                        "agent_id": cfg.AgentID,
                        "region": cfg.Region,
                        "method": "http",
                        "success": ok,
                        "latency_ms": lat,
                        "status_code": code,
                        "message": msg,
                        "details": map[string]any{"headers": hdrs},
                        "checked_at": time.Now().UTC().Format(time.RFC3339Nano),
                    })
                case "dns":
                    ok, lat, msg, det := dnsCheck(job.Target)
                    _ = postResult(ctx, cfg, map[string]any{
                        "task_id": job.TaskID.String(),
                        "agent_id": cfg.AgentID,
                        "region": cfg.Region,
                        "method": "dns",
                        "success": ok,
                        "latency_ms": lat,
                        "status_code": 0,
                        "message": msg,
                        "details": det,
                        "checked_at": time.Now().UTC().Format(time.RFC3339Nano),
                    })
                case "tcp":
                    ok, lat, msg := tcpCheck(job.Target)
                    _ = postResult(ctx, cfg, map[string]any{
                        "task_id": job.TaskID.String(),
                        "agent_id": cfg.AgentID,
                        "region": cfg.Region,
                        "method": "tcp",
                        "success": ok,
                        "latency_ms": lat,
                        "status_code": 0,
                        "message": msg,
                        "checked_at": time.Now().UTC().Format(time.RFC3339Nano),
                    })
                case "icmp":
                    ok, lat, msg := icmpCheck(job.Target)
                    _ = postResult(ctx, cfg, map[string]any{
                        "task_id": job.TaskID.String(),
                        "agent_id": cfg.AgentID,
                        "region": cfg.Region,
                        "method": "icmp",
                        "success": ok,
                        "latency_ms": lat,
                        "status_code": 0,
                        "message": msg,
                        "checked_at": time.Now().UTC().Format(time.RFC3339Nano),
                    })
                case "udp":
                    ok, lat, msg := udpCheck(job.Target)
                    _ = postResult(ctx, cfg, map[string]any{
                        "task_id": job.TaskID.String(),
                        "agent_id": cfg.AgentID,
                        "region": cfg.Region,
                        "method": "udp",
                        "success": ok,
                        "latency_ms": lat,
                        "status_code": 0,
                        "message": msg,
                        "checked_at": time.Now().UTC().Format(time.RFC3339Nano),
                    })
                case "whois":
                    ok, lat, msg := whoisCheck(job.Target)
                    geo := geoIPLookup(job.Target)
                    _ = postResult(ctx, cfg, map[string]any{
                        "task_id": job.TaskID.String(),
                        "agent_id": cfg.AgentID,
                        "region": cfg.Region,
                        "method": "whois",
                        "success": ok,
                        "latency_ms": lat,
                        "status_code": 0,
                        "message": msg,
                        "details": map[string]any{"geoip": geo},
                        "checked_at": time.Now().UTC().Format(time.RFC3339Nano),
                    })
                case "traceroute":
                    ok, lat, msg, hops := traceroute(job.Target)
                    geo := geoIPLookup(job.Target)
                    _ = postResult(ctx, cfg, map[string]any{
                        "task_id": job.TaskID.String(),
                        "agent_id": cfg.AgentID,
                        "region": cfg.Region,
                        "method": "traceroute",
                        "success": ok,
                        "latency_ms": lat,
                        "status_code": 0,
                        "message": msg,
                        "details": map[string]any{"hops": hops, "geoip": geo},
                        "checked_at": time.Now().UTC().Format(time.RFC3339Nano),
                    })
            }
            sendLog(ctx, cfg, job.TaskID.String(), m, "Готово")
        }
    }
}


