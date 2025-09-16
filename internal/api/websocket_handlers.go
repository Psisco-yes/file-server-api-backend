package api

import (
	"log"
	"net/http"
	"serwer-plikow/internal/auth"
	"serwer-plikow/internal/websocket"
)

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
