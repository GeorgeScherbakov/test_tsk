package repository

import (
	"context"
	"posts-service/internal/model"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Errors
type ErrPostExists struct {
	PostID string
}

func (e *ErrPostExists) Error() string {
	return "post already exists: " + e.PostID
}

type ErrPostNotFound struct {
	PostID string
}

func (e *ErrPostNotFound) Error() string {
	return "post not found: " + e.PostID
}

type ErrCommentTooLong struct {
	MaxLength int
}

func (e *ErrCommentTooLong) Error() string {
	return "comment too long: max " + strconv.Itoa(e.MaxLength) + " characters"
}

type ErrCommentsDisabled struct {
	PostID string
}

func (e *ErrCommentsDisabled) Error() string {
	return "comments are disabled for this post: " + e.PostID
}

type MemoryStore struct {
	mu          sync.RWMutex
	posts       map[string]*model.Post
	comments    map[string]*model.Comment
	byPost      map[string][]string
	postRoots   map[string][]string
	byParent    map[string][]string
	subscribers map[string][]chan *model.Comment
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		posts:       make(map[string]*model.Post),
		comments:    make(map[string]*model.Comment),
		byPost:      make(map[string][]string),
		postRoots:   make(map[string][]string),
		byParent:    make(map[string][]string),
		subscribers: make(map[string][]chan *model.Comment),
	}
}

//---------------------------------------Posts---------------------------------------------

func (s *MemoryStore) CreatePost(ctx context.Context, post *model.Post) error {
	if err := checkContext(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if post.ID == "" {
		post.ID = generateID()
	} else if _, exists := s.posts[post.ID]; exists {
		return &ErrPostExists{PostID: post.ID}
	}
	post.CreatedAt = time.Now()
	s.posts[post.ID] = post
	return nil
}

func (s *MemoryStore) GetPostByID(ctx context.Context, id string) (*model.Post, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	post, ok := s.posts[id]
	if !ok {
		return nil, &ErrPostNotFound{PostID: id}
	}
	return post, nil
}

func (s *MemoryStore) GetPosts(ctx context.Context) ([]*model.Post, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	posts := make([]*model.Post, 0, len(s.posts))
	for _, p := range s.posts {
		posts = append(posts, p)
	}

	sort.Slice(posts, func(i, j int) bool {
		return posts[i].CreatedAt.After(posts[j].CreatedAt)
	})

	return posts, nil
}

func (s *MemoryStore) ToggleComments(ctx context.Context, postID string, allowComments bool) (*model.Post, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	post, ok := s.posts[postID]
	if !ok {
		return nil, &ErrPostNotFound{PostID: postID}
	}
	post.AllowComments = allowComments
	return post, nil
}

//---------------------------------------Comments---------------------------------------------

func (s *MemoryStore) CreateComment(ctx context.Context, comment *model.Comment) (*model.Comment, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(comment.Content) > 2000 {
		return nil, &ErrCommentTooLong{MaxLength: 2000}
	}

	post, ok := s.posts[comment.PostID]
	if !ok {
		return nil, &ErrPostNotFound{PostID: comment.PostID}
	}

	if !post.AllowComments {
		return nil, &ErrCommentsDisabled{PostID: comment.PostID}
	}

	comment.ID = generateID()
	comment.CreatedAt = time.Now()

	s.comments[comment.ID] = comment
	s.byPost[comment.PostID] = append(s.byPost[comment.PostID], comment.ID)

	if comment.ParentID == nil {
		s.postRoots[comment.PostID] = append(s.postRoots[comment.PostID], comment.ID)
	} else {
		s.byParent[*comment.ParentID] = append(s.byParent[*comment.ParentID], comment.ID)
	}

	return comment, nil
}

func (s *MemoryStore) GetCommentsByPostID(ctx context.Context, postID string, first int, after *string) (*model.CommentConnection, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rootIDs, ok := s.postRoots[postID]
	if !ok || len(rootIDs) == 0 {
		return &model.CommentConnection{
			Edges:      []*model.CommentEdge{},
			PageInfo:   &model.PageInfo{HasNextPage: false},
			TotalCount: 0,
		}, nil
	}

	return s.paginateComments(rootIDs, first, after, true)
}

func (s *MemoryStore) GetRepliesByParentID(ctx context.Context, parentID string, first int, after *string) (*model.CommentConnection, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	replyIDs, ok := s.byParent[parentID]
	if !ok || len(replyIDs) == 0 {
		return &model.CommentConnection{
			Edges:      []*model.CommentEdge{},
			PageInfo:   &model.PageInfo{HasNextPage: false},
			TotalCount: 0,
		}, nil
	}

	return s.paginateComments(replyIDs, first, after, false)
}

//---------------------------------------Auxiliary functions---------------------------------------------

func generateID() string {
	return uuid.New().String()
}

func checkContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (s *MemoryStore) paginateComments(
	commentIDs []string,
	first int,
	after *string,
	sortNewestFirst bool,
) (*model.CommentConnection, error) {
	comments := make([]*model.Comment, 0, len(commentIDs))
	for _, id := range commentIDs {
		if c, exists := s.comments[id]; exists {
			comments = append(comments, c)
		}
	}

	if len(comments) == 0 {
		return &model.CommentConnection{
			Edges:      []*model.CommentEdge{},
			PageInfo:   &model.PageInfo{HasNextPage: false},
			TotalCount: 0,
		}, nil
	}

	sort.Slice(comments, func(i, j int) bool {
		if sortNewestFirst {
			return comments[i].CreatedAt.After(comments[j].CreatedAt)
		}
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})

	start := 0
	if after != nil && *after != "" {
		for i, c := range comments {
			if c.ID == *after {
				start = i + 1
				break
			}
		}
	}

	end := start + first
	if end > len(comments) {
		end = len(comments)
	}
	paginated := comments[start:end]

	edges := make([]*model.CommentEdge, len(paginated))
	for i, c := range paginated {
		edges[i] = &model.CommentEdge{
			Node:   c,
			Cursor: c.ID,
		}
	}

	hasNextPage := end < len(comments)
	var endCursor *string
	if len(edges) > 0 {
		endCursor = &edges[len(edges)-1].Cursor
	}

	return &model.CommentConnection{
		Edges:      edges,
		PageInfo:   &model.PageInfo{HasNextPage: hasNextPage, EndCursor: endCursor},
		TotalCount: len(comments),
	}, nil
}
