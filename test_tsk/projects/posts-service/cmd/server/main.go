package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"posts-service/internal/graph"
	"posts-service/internal/repository"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
)

func main() {
	storage := os.Getenv("STORAGE")
	if storage == "" {
		storage = "memory"
		log.Println("STORAGE not set, defaulting to 'memory'")
	}
	log.Printf("Starting with storage: %s", storage)

	var (
		postRepo    repository.PostRepository
		commentRepo repository.CommentRepository
		closeFn     = func() {}
	)

	switch storage {
	case "postgres":
		connString := os.Getenv("DATABASE_URL")
		if connString == "" {
			connString = "postgresql://postgres@localhost:5432/postsdb?sslmode=disable"
			log.Printf("DATABASE_URL not set, using default: %s", connString)
		}
		pgStore, err := repository.NewPostgresStore(connString)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		postRepo = pgStore
		commentRepo = pgStore
		closeFn = pgStore.Close
		log.Println("PostgreSQL connected")

	default:
		mem := repository.NewMemoryStore()
		postRepo = mem
		commentRepo = mem
		log.Println("Memory store ready")
	}
	defer closeFn()

	resolver := graph.NewResolver(postRepo, commentRepo)
	srv := handler.NewDefaultServer(
		graph.NewExecutableSchema(graph.Config{Resolvers: resolver}),
	)

	mux := http.NewServeMux()
	mux.Handle("/", playground.Handler("GraphQL playground", "/query"))
	mux.Handle("/query", srv)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	httpSrv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	log.Printf("Server on http://localhost:%s/ (storage: %s)", port, storage)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}