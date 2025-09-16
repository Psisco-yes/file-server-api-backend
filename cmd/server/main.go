// @title           File Server API
// @version         1.0
// @host            localhost
// @schemes         http https
// @BasePath        /api/v1
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
package main

import (
	"context"
	"log"
	"net/http"
	"serwer-plikow/internal/api"
	"serwer-plikow/internal/config"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/storage"
	"serwer-plikow/internal/websocket"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "serwer-plikow/docs"

	httpSwagger "github.com/swaggo/http-swagger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Nie można wczytać konfiguracji: %v", err)
	}

	dbpool, err := pgxpool.New(context.Background(), cfg.DB.Source)
	if err != nil {
		log.Fatalf("Nie można połączyć się z bazą danych: %v", err)
	}
	defer dbpool.Close()

	if err := dbpool.Ping(context.Background()); err != nil {
		log.Fatalf("Nie można pingować bazy danych: %v", err)
	}
	log.Println("Pomyślnie połączono z bazą danych")

	localStorage, err := storage.NewLocalStorage(cfg.Storage.Path)
	if err != nil {
		log.Fatalf("Nie można zainicjować local storage: %v", err)
	}
	log.Printf("Pliki będą przechowywane w: %s", cfg.Storage.Path)

	wsHub := websocket.NewHub()
	go wsHub.Run()

	store := database.NewStore(dbpool, wsHub)
	server := api.NewServer(cfg, store, localStorage, wsHub)

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(api.MetricsMiddleware)

	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("https://localhost/swagger/doc.json"),
	))

	r.Get("/ws", server.ServeWsHandler)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Serwer plików działa! Dokumentacja dostępna pod /swagger/index.html"))
	})

	r.Post("/api/v1/auth/login", server.LoginHandler)

	r.Get("/health", server.HealthCheckHandler)
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(server.AuthMiddleware)
		r.Get("/me", server.GetCurrentUserHandler)
		r.Get("/nodes", server.ListNodesHandler)
		r.Post("/nodes/folder", server.CreateFolderHandler)
		r.Post("/nodes/file", server.UploadFileHandler)
		r.Get("/nodes/{nodeId}/download", server.DownloadFileHandler)
		r.Patch("/nodes/{nodeId}", server.UpdateNodeHandler)
		r.Delete("/nodes/{nodeId}", server.DeleteNodeHandler)
		r.Post("/nodes/{nodeId}/restore", server.RestoreNodeHandler)
		r.Post("/nodes/{nodeId}/favorite", server.AddFavoriteHandler)
		r.Delete("/nodes/{nodeId}/favorite", server.RemoveFavoriteHandler)
		r.Get("/nodes/archive", server.DownloadArchiveHandler)
		r.Get("/trash", server.ListTrashHandler)
		r.Delete("/trash/purge", server.PurgeTrashHandler)
		r.Get("/favorites", server.ListFavoritesHandler)
		r.Post("/nodes/{nodeId}/share", server.ShareNodeHandler)
		r.Get("/shares/incoming/users", server.ListSharingUsersHandler)
		r.Get("/shares/incoming/nodes", server.ListSharedNodesHandler)
		r.Get("/shares/outgoing", server.ListOutgoingSharesHandler)
		r.Delete("/shares/{shareId}", server.DeleteShareHandler)
		r.Get("/events", server.GetEventsHandler)
	})

	log.Println("Uruchamianie serwera na porcie :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("Nie można uruchomić serwera: %v", err)
	}
}
