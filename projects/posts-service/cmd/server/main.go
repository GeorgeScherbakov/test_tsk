package main

import (
	"log"
	"net/http"
	"os"

	"posts-service/internal/graph"
	"posts-service/internal/repository"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
)

func main() {
	// Определяем тип хранилища
	storage := os.Getenv("STORAGE")
	if storage == "" {
		storage = "memory"
		log.Println("No STORAGE specified, using 'memory' as default")
	}

	log.Printf("Starting server with storage: %s", storage)

	// Создаём репозитории в зависимости от типа хранилища
	var postRepo repository.PostRepository
	var commentRepo repository.CommentRepository

	switch storage {
	case "postgres":
		connString := os.Getenv("DATABASE_URL")
		if connString == "" {
			connString = "postgresql://postgres@localhost:5432/postsdb?sslmode=disable"
			log.Printf("No DATABASE_URL specified, using default: %s", connString)
		}

		pgStore, err := repository.NewPostgresStore(connString)
		if err != nil {
			log.Fatalf("Failed to create postgres store: %v", err)
		}
		defer pgStore.Close()

		postRepo = pgStore
		commentRepo = pgStore
		log.Println("PostgreSQL store connected")

	default: // memory
		memoryStore := repository.NewMemoryStore()
		postRepo = memoryStore
		commentRepo = memoryStore
		log.Println("Memory store created")
	}

	// Создаём резолвер с репозиториями
	resolver := graph.NewResolver(postRepo, commentRepo)

	// Создаём GraphQL сервер
	srv := handler.NewDefaultServer(
		graph.NewExecutableSchema(graph.Config{Resolvers: resolver}),
	)

	// Настраиваем маршруты
	http.Handle("/", playground.Handler("GraphQL playground", "/query"))
	http.Handle("/query", srv)

	// Запускаем сервер
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server started on http://localhost:%s/ (storage: %s)", port, storage)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
