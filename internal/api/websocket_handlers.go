package api

import (
	"log"
	"net/http"
	"serwer-plikow/internal/auth"
	"serwer-plikow/internal/websocket"
)

// @Summary      Establish WebSocket connection
// @Description  Upgrades the HTTP connection to a WebSocket connection for real-time event notifications. The authentication token must be provided as a query parameter.
// @Tags         websockets
// @Param        token  query     string  true  "JWT authentication token"
// @Success      101    {string}  string  "Switching Protocols"
// @Failure      401    {string}  string  "Unauthorized - Invalid or missing token"
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

	conn, err := websocket.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}

	client := websocket.NewClient(s.wsHub, conn, claims.UserID)
	s.wsHub.Register <- client

	go client.ReadPump()
	go client.WritePump()
}
