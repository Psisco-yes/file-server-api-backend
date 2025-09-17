// @title           File Server API
// @version         1.0
// @description     A comprehensive file server API built with Go. It supports file and folder management, sharing, real-time updates via WebSockets, and more. All protected endpoints require a Bearer Token for authorization.
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

	"github.com/go-chi/cors"

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

	store := database.NewStore(dbpool)
	server := api.NewServer(cfg, store, localStorage, wsHub)

	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(api.MetricsMiddleware)

	r.Get("/swagger/*", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	r.Get("/ws", server.ServeWsHandler)
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Serwer plików działa! Dokumentacja dostępna pod /swagger/index.html"))
	})
	r.Get("/health", server.HealthCheckHandler)
	r.Get("/metrics", metricsHandler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", server.LoginHandler)

		r.Group(func(r chi.Router) {
			r.Use(server.AuthMiddleware)

			r.Route("/me", func(r chi.Router) {
				r.Get("/", server.GetCurrentUserHandler)
				r.Get("/storage", server.GetStorageUsageHandler)
			})

			r.Route("/nodes", func(r chi.Router) {
				r.Get("/", server.ListNodesHandler)
				r.Post("/folder", server.CreateFolderHandler)
				r.Post("/file", server.UploadFileHandler)
				r.Get("/archive", server.DownloadArchiveHandler)

				r.Route("/{nodeId}", func(r chi.Router) {
					r.Get("/download", server.DownloadFileHandler)
					r.Patch("/", server.UpdateNodeHandler)
					r.Delete("/", server.DeleteNodeHandler)
					r.Post("/restore", server.RestoreNodeHandler)
					r.Post("/favorite", server.AddFavoriteHandler)
					r.Delete("/favorite", server.RemoveFavoriteHandler)
					r.Post("/share", server.ShareNodeHandler)
				})
			})

			r.Route("/shares", func(r chi.Router) {
				r.Get("/incoming/users", server.ListSharingUsersHandler)
				r.Get("/incoming/nodes", server.ListSharedNodesHandler)
				r.Get("/outgoing", server.ListOutgoingSharesHandler)
				r.Delete("/{shareId}", server.DeleteShareHandler)
			})

			r.Route("/trash", func(r chi.Router) {
				r.Get("/", server.ListTrashHandler)
				r.Delete("/purge", server.PurgeTrashHandler)
			})

			r.Get("/favorites", server.ListFavoritesHandler)

			r.Get("/events", server.GetEventsHandler)
		})
	})

	log.Println("Uruchamianie serwera na porcie :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("Nie można uruchomić serwera: %v", err)
	}
}

func metricsHandler() http.HandlerFunc {
	return promhttp.Handler().ServeHTTP
}
