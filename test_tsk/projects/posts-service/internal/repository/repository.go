package repository

import (
	"context"

	"posts-service/internal/model"
)

type PostRepository interface {
	CreatePost(ctx context.Context, post *model.Post) error
	GetPostByID(ctx context.Context, id string) (*model.Post, error)
	// GetPosts теперь принимает first/after для пагинации
	GetPosts(ctx context.Context, first int, after *string) (*model.PostConnection, error)
	ToggleComments(ctx context.Context, postID string, allowComments bool) (*model.Post, error)
}

type CommentRepository interface {
	CreateComment(ctx context.Context, comment *model.Comment) (*model.Comment, error)
	GetCommentsByPostID(ctx context.Context, postID string, first int, after *string) (*model.CommentConnection, error)
	GetRepliesByParentID(ctx context.Context, parentID string, first int, after *string) (*model.CommentConnection, error)
	Subscribe(ctx context.Context, postID string) (<-chan *model.Comment, error)
}