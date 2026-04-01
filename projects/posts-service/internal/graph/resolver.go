package graph

import (
	"posts-service/internal/repository"
)

type Resolver struct {
	PostRepo    repository.PostRepository
	CommentRepo repository.CommentRepository
}

func NewResolver(postRepo repository.PostRepository, commentRepo repository.CommentRepository) *Resolver {
	return &Resolver{
		PostRepo:    postRepo,
		CommentRepo: commentRepo,
	}
}
