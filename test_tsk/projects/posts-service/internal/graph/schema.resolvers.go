package graph

import (
	"context"
	"fmt"

	"posts-service/internal/model"
)

// ─── Comment ─────────────────────────────────────────────────────────────────

func (r *commentResolver) Replies(ctx context.Context, obj *model.Comment, first *int, after *string) (*model.CommentConnection, error) {
	limit := 10
	if first != nil && *first > 0 {
		limit = *first
	}
	return r.CommentRepo.GetRepliesByParentID(ctx, obj.ID, limit, after)
}

func (r *commentResolver) CreatedAt(ctx context.Context, obj *model.Comment) (string, error) {
	return obj.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), nil
}

// ─── Mutation ────────────────────────────────────────────────────────────────

func (r *mutationResolver) CreatePost(ctx context.Context, title string, content string, author string) (*model.Post, error) {
	if title == "" {
		return nil, fmt.Errorf("title cannot be empty")
	}
	if content == "" {
		return nil, fmt.Errorf("content cannot be empty")
	}
	if author == "" {
		return nil, fmt.Errorf("author cannot be empty")
	}

	post := &model.Post{
		Title:   title,
		Content: content,
		Author:  author,
	}
	if err := r.PostRepo.CreatePost(ctx, post); err != nil {
		return nil, err
	}
	return post, nil
}

func (r *mutationResolver) CreateComment(ctx context.Context, input model.CreateCommentInput) (*model.Comment, error) {
	if input.Author == "" {
		return nil, fmt.Errorf("author cannot be empty")
	}
	if input.Content == "" {
		return nil, fmt.Errorf("content cannot be empty")
	}

	comment := &model.Comment{
		PostID:   input.PostID,
		ParentID: input.ParentID,
		Author:   input.Author,
		Content:  input.Content,
	}
	return r.CommentRepo.CreateComment(ctx, comment)
}

func (r *mutationResolver) ToggleComments(ctx context.Context, postID string, allowComments bool) (*model.Post, error) {
	return r.PostRepo.ToggleComments(ctx, postID, allowComments)
}

// ─── Post ────────────────────────────────────────────────────────────────────

func (r *postResolver) Comments(ctx context.Context, obj *model.Post, first *int, after *string) (*model.CommentConnection, error) {
	limit := 10
	if first != nil && *first > 0 {
		limit = *first
	}
	return r.CommentRepo.GetCommentsByPostID(ctx, obj.ID, limit, after)
}

func (r *postResolver) CreatedAt(ctx context.Context, obj *model.Post) (string, error) {
	return obj.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), nil
}

// ─── Query ───────────────────────────────────────────────────────────────────

func (r *queryResolver) Posts(ctx context.Context, first *int, after *string) (*model.PostConnection, error) {
	limit := 10
	if first != nil && *first > 0 {
		limit = *first
	}
	return r.PostRepo.GetPosts(ctx, limit, after)
}

func (r *queryResolver) Post(ctx context.Context, id string) (*model.Post, error) {
	return r.PostRepo.GetPostByID(ctx, id)
}

// ─── Subscription ────────────────────────────────────────────────────────────

// CommentAdded подписывается на новые комментарии к посту.
// Канал закрывается при отмене ctx (клиент отключился).
func (r *subscriptionResolver) CommentAdded(ctx context.Context, postID string) (<-chan *model.Comment, error) {
	return r.CommentRepo.Subscribe(ctx, postID)
}

// ─── Resolver wiring ─────────────────────────────────────────────────────────

func (r *Resolver) Comment() CommentResolver          { return &commentResolver{r} }
func (r *Resolver) Mutation() MutationResolver        { return &mutationResolver{r} }
func (r *Resolver) Post() PostResolver                { return &postResolver{r} }
func (r *Resolver) Query() QueryResolver              { return &queryResolver{r} }
func (r *Resolver) Subscription() SubscriptionResolver { return &subscriptionResolver{r} }

type commentResolver      struct{ *Resolver }
type mutationResolver     struct{ *Resolver }
type postResolver         struct{ *Resolver }
type queryResolver        struct{ *Resolver }
type subscriptionResolver struct{ *Resolver }