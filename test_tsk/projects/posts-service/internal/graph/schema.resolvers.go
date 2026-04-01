package graph

import (
	"context"
	"posts-service/internal/model"
)

// Replies is the resolver for the replies field.
func (r *commentResolver) Replies(ctx context.Context, obj *model.Comment, first *int, after *string) (*model.CommentConnection, error) {
	limit := 10
	if first != nil {
		limit = *first
	}
	return r.CommentRepo.GetRepliesByParentID(ctx, obj.ID, limit, after)
}

// CreatedAt is the resolver for the createdAt field.
func (r *commentResolver) CreatedAt(ctx context.Context, obj *model.Comment) (string, error) {
	return obj.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), nil
}

// CreatePost is the resolver for the createPost field.
func (r *mutationResolver) CreatePost(ctx context.Context, title string, content string, author string) (*model.Post, error) {
	post := &model.Post{
		Title:   title,
		Content: content,
		Author:  author,
	}
	err := r.PostRepo.CreatePost(ctx, post)
	if err != nil {
		return nil, err
	}
	return post, nil
}

// CreateComment is the resolver for the createComment field.
func (r *mutationResolver) CreateComment(ctx context.Context, input model.CreateCommentInput) (*model.Comment, error) {
	comment := &model.Comment{
		PostID:   input.PostID,
		ParentID: input.ParentID,
		Author:   input.Author,
		Content:  input.Content,
	}
	return r.CommentRepo.CreateComment(ctx, comment)
}

// ToggleComments is the resolver for the toggleComments field.
func (r *mutationResolver) ToggleComments(ctx context.Context, postID string, allowComments bool) (*model.Post, error) {
	return r.PostRepo.ToggleComments(ctx, postID, allowComments)
}

// Comments is the resolver for the comments field.
func (r *postResolver) Comments(ctx context.Context, obj *model.Post, first *int, after *string) (*model.CommentConnection, error) {
	limit := 10
	if first != nil {
		limit = *first
	}
	return r.CommentRepo.GetCommentsByPostID(ctx, obj.ID, limit, after)
}

// CreatedAt is the resolver for the createdAt field.
func (r *postResolver) CreatedAt(ctx context.Context, obj *model.Post) (string, error) {
	return obj.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), nil
}

// Posts is the resolver for the posts field.
func (r *queryResolver) Posts(ctx context.Context, first *int, after *string) (*model.PostConnection, error) {
	posts, err := r.PostRepo.GetPosts(ctx)
	if err != nil {
		return nil, err
	}

	edges := make([]*model.PostEdge, len(posts))
	for i, p := range posts {
		edges[i] = &model.PostEdge{
			Node:   p,
			Cursor: p.ID,
		}
	}

	var endCursor *string
	if len(edges) > 0 {
		endCursor = &edges[len(edges)-1].Cursor
	}

	return &model.PostConnection{
		Edges: edges,
		PageInfo: &model.PageInfo{
			HasNextPage: false,
			EndCursor:   endCursor,
		},
		TotalCount: len(posts),
	}, nil
}

// Post is the resolver for the post field.
func (r *queryResolver) Post(ctx context.Context, id string) (*model.Post, error) {
	return r.PostRepo.GetPostByID(ctx, id)
}

// CommentAdded is the resolver for the commentAdded field.
func (r *subscriptionResolver) CommentAdded(ctx context.Context, postID string) (<-chan *model.Comment, error) {
    return r.CommentRepo.Subscribe(ctx, postID)
}

// Comment returns CommentResolver implementation.
func (r *Resolver) Comment() CommentResolver { return &commentResolver{r} }

// Mutation returns MutationResolver implementation.
func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }

// Post returns PostResolver implementation.
func (r *Resolver) Post() PostResolver { return &postResolver{r} }

// Query returns QueryResolver implementation.
func (r *Resolver) Query() QueryResolver { return &queryResolver{r} }

// Subscription returns SubscriptionResolver implementation.
func (r *Resolver) Subscription() SubscriptionResolver { return &subscriptionResolver{r} }

type commentResolver struct{ *Resolver }
type mutationResolver struct{ *Resolver }
type postResolver struct{ *Resolver }
type queryResolver struct{ *Resolver }
type subscriptionResolver struct{ *Resolver }
