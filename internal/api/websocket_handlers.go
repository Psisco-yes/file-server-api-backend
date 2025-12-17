package api

import (
	"log"
	"net/http"
	"serwer-plikow/internal/auth"
	"serwer-plikow/internal/websocket"

	ws "github.com/gorilla/websocket"
)

var upgrader = ws.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// @Summary      Establish WebSocket connection
// @Description  Upgrades an HTTP connection to a WebSocket connection for real-time updates. The access token must be provided as a query parameter.
// @Tags         websocket
// @Param        token  query     string  true  "JWT Access Token"
// @Success      101    {string}  string  "Switching Protocols"
// @Failure      401    {string}  string  "Unauthorized - Invalid or missing token"
// @Failure      429    {string}  string "Too Many Requests"
// @Router       /ws [get]
func (s *Server) ServeWsHandler(w http.ResponseWriter, r *http.Request) {
	tokenString := r.URL.Query().Get("token")
	if tokenString == "" {
		log.Println("WS connection attempt without token")
		return
	}

	claims, err := auth.VerifyJWT(tokenString, s.config.JWT.Secret)
	if err != nil {
		log.Printf("WS connection attempt with invalid token: %v", err)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}

	client := websocket.NewClient(s.wsHub, conn, claims.UserID)
	s.wsHub.Register <- client

	go client.ReadPump()
	go client.WritePump()
}
