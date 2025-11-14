package httpserver

import (
    "net/http"
    "sync"

    "github.com/gin-gonic/gin"
    "github.com/gorilla/websocket"
)

type wsHub struct {
    mu    sync.RWMutex
    conns map[*websocket.Conn]struct{}
}

func newHub() *wsHub { return &wsHub{conns: make(map[*websocket.Conn]struct{})} }

func (h *wsHub) broadcast(msg []byte) {
    h.mu.RLock()
    defer h.mu.RUnlock()
    for c := range h.conns {
        _ = c.WriteMessage(websocket.TextMessage, msg)
    }
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (s *Server) wsHandler(c *gin.Context) {
    conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
    if err != nil {
        c.Status(http.StatusBadRequest)
        return
    }
    if s.hub == nil {
        s.hub = newHub()
    }
    s.hub.mu.Lock()
    s.hub.conns[conn] = struct{}{}
    s.hub.mu.Unlock()

    for {
        if _, _, err := conn.ReadMessage(); err != nil {
            s.hub.mu.Lock()
            delete(s.hub.conns, conn)
            s.hub.mu.Unlock()
            _ = conn.Close()
            return
        }
    }
}




