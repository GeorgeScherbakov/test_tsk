package service

import (
	"context"
	"errors"

	"posts-service/internal/model"
	"posts-service/internal/repository"
)

type Service struct {
	posts    repository.PostRepository
	comments repository.CommentRepository
	pubsub   *repository.PubSub
}

func NewService(posts repository.PostRepository, comments repository.CommentRepository, pubsub *repository.PubSub) *Service {
	return &Service{
		posts:    posts,
		comments: comments,
		pubsub:   pubsub,
	}
}

// Posts

func (s *Service) CreatePost(ctx context.Context, title, content, author string) (*model.Post, error) {
	if title == "" {
		return nil, errors.New("title cannot be empty")
	}
	if content == "" {
		return nil, errors.New("content cannot be empty")
	}
	if author == "" {
		return nil, errors.New("author cannot be empty")
	}

	post := &model.Post{
		Title:         title,
		Content:       content,
		Author:        author,
		AllowComments: true,
	}

	return s.posts.CreatePost(ctx, post)
}

func (s *Service) GetPostByID(ctx context.Context, id string) (*model.Post, error) {
	if id == "" {
		return nil, errors.New("id cannot be empty")
	}
	return s.posts.GetPostByID(ctx, id)
}

func (s *Service) GetPosts(ctx context.Context, first int, after string) (*model.PostConnection, error) {
	return s.posts.GetPosts(ctx, first, after)
}

func (s *Service) ToggleComments(ctx context.Context, postID string, allowComments bool) (*model.Post, error) {
	if postID == "" {
		return nil, errors.New("postID cannot be empty")
	}
	return s.posts.ToggleComments(ctx, postID, allowComments)
}

// Comments

func (s *Service) CreateComment(ctx context.Context, postID string, parentID *string, author, content string) (*model.Comment, error) {
	if postID == "" {
		return nil, errors.New("postID cannot be empty")
	}
	if author == "" {
		return nil, errors.New("author cannot be empty")
	}
	if content == "" {
		return nil, errors.New("content cannot be empty")
	}
	if len(content) > 2000 {
		return nil, errors.New("comment too long: max 2000 characters")
	}

	// проверяем что пост существует и комментарии разрешены
	post, err := s.posts.GetPostByID(ctx, postID)
	if err != nil {
		return nil, err
	}
	if !post.AllowComments {
		return nil, errors.New("comments are disabled for this post")
	}

	comment := &model.Comment{
		PostID:   postID,
		ParentID: parentID,
		Author:   author,
		Content:  content,
	}

	created, err := s.comments.CreateComment(ctx, comment)
	if err != nil {
		return nil, err
	}

	// уведомляем подписчиков
	s.pubsub.Publish(postID, created)

	return created, nil
}

func (s *Service) GetCommentsByPostID(ctx context.Context, postID string, first int, after string) (*model.CommentConnection, error) {
	if postID == "" {
		return nil, errors.New("postID cannot be empty")
	}
	return s.comments.GetCommentsByPostID(ctx, postID, first, after)
}

func (s *Service) GetRepliesByParentID(ctx context.Context, parentID string, first int, after string) (*model.CommentConnection, error) {
	if parentID == "" {
		return nil, errors.New("parentID cannot be empty")
	}
	return s.comments.GetRepliesByParentID(ctx, parentID, first, after)
}

func (s *Service) Subscribe(postID string) chan *model.Comment {
	return s.pubsub.Subscribe(postID)
}

func (s *Service) Unsubscribe(postID string, ch chan *model.Comment) {
	s.pubsub.Unsubscribe(postID, ch)
}
