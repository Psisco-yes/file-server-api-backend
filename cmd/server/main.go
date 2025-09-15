package main

import (
	"context"
	"log"
	"net/http"
	"serwer-plikow/internal/api"
	"serwer-plikow/internal/config"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/storage"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
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

	store := database.NewStore(dbpool)
	server := api.NewServer(cfg, store, localStorage)

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Group(func(r chi.Router) {
		r.Post("/api/v1/auth/login", server.LoginHandler)
		r.Get("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("Serwer plików działa!"))
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(server.AuthMiddleware)
		r.Get("/api/v1/me", server.GetCurrentUserHandler)
		r.Post("/api/v1/nodes/folder", server.CreateFolderHandler)
		r.Get("/api/v1/nodes", server.ListNodesHandler)
		r.Post("/api/v1/nodes/file", server.UploadFileHandler)
		r.Get("/api/v1/nodes/{nodeId}/download", server.DownloadFileHandler)
		r.Delete("/api/v1/nodes/{nodeId}", server.DeleteNodeHandler)
		r.Delete("/api/v1/trash/purge", server.PurgeTrashHandler)
		r.Patch("/api/v1/nodes/{nodeId}", server.UpdateNodeHandler)
		r.Get("/api/v1/trash", server.ListTrashHandler)
		r.Post("/api/v1/nodes/{nodeId}/restore", server.RestoreNodeHandler)
		r.Get("/api/v1/nodes/archive", server.DownloadArchiveHandler)
		r.Post("/api/v1/nodes/{nodeId}/share", server.ShareNodeHandler)
		r.Get("/api/v1/shares/incoming/users", server.ListSharingUsersHandler)
		r.Get("/api/v1/shares/incoming/nodes", server.ListSharedNodesHandler)
	})

	log.Println("Uruchamianie serwera na porcie :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("Nie można uruchomić serwera: %v", err)
	}
}
