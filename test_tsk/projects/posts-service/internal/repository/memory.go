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
 
// ─── Errors ──────────────────────────────────────────────────────────────────
 
type ErrPostExists struct{ PostID string }
 
func (e *ErrPostExists) Error() string { return "post already exists: " + e.PostID }
 
type ErrPostNotFound struct{ PostID string }
 
func (e *ErrPostNotFound) Error() string { return "post not found: " + e.PostID }
 
type ErrCommentTooLong struct{ MaxLength int }
 
func (e *ErrCommentTooLong) Error() string {
	return "comment too long: max " + strconv.Itoa(e.MaxLength) + " characters"
}
 
type ErrCommentsDisabled struct{ PostID string }
 
func (e *ErrCommentsDisabled) Error() string {
	return "comments are disabled for this post: " + e.PostID
}
 
// ─── MemoryStore ─────────────────────────────────────────────────────────────
 
type MemoryStore struct {
	mu          sync.RWMutex
	posts       map[string]*model.Post
	comments    map[string]*model.Comment
	postRoots   map[string][]string // postID   → корневые commentID (ordered by insert)
	byParent    map[string][]string // parentID → дочерние commentID (ordered by insert)
	subscribers map[string][]chan *model.Comment
}
 
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		posts:       make(map[string]*model.Post),
		comments:    make(map[string]*model.Comment),
		postRoots:   make(map[string][]string),
		byParent:    make(map[string][]string),
		subscribers: make(map[string][]chan *model.Comment),
	}
}
 
// ─── Posts ───────────────────────────────────────────────────────────────────
 
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
 
	post.AllowComments = true // дефолт всегда true
	post.CreatedAt = time.Now()
 
	clone := *post
	s.posts[post.ID] = &clone
	*post = clone // обновляем оригинал (ID, CreatedAt)
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
	clone := *post
	return &clone, nil
}
 
// GetPosts возвращает посты с cursor-based пагинацией (cursor = ID поста).
func (s *MemoryStore) GetPosts(ctx context.Context, first int, after *string) (*model.PostConnection, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if first <= 0 {
		first = 10
	}
 
	s.mu.RLock()
	defer s.mu.RUnlock()
 
	posts := make([]*model.Post, 0, len(s.posts))
	for _, p := range s.posts {
		clone := *p
		posts = append(posts, &clone)
	}
 
	sort.Slice(posts, func(i, j int) bool {
		return posts[i].CreatedAt.After(posts[j].CreatedAt)
	})
 
	// Находим стартовую позицию после курсора
	start := 0
	if after != nil && *after != "" {
		for i, p := range posts {
			if p.ID == *after {
				start = i + 1
				break
			}
		}
	}
 
	end := start + first
	if end > len(posts) {
		end = len(posts)
	}
	paginated := posts[start:end]
 
	edges := make([]*model.PostEdge, len(paginated))
	for i, p := range paginated {
		edges[i] = &model.PostEdge{Node: p, Cursor: p.ID}
	}
 
	hasNextPage := end < len(posts)
	var endCursor *string
	if len(edges) > 0 {
		endCursor = &edges[len(edges)-1].Cursor
	}
 
	return &model.PostConnection{
		Edges:      edges,
		PageInfo:   &model.PageInfo{HasNextPage: hasNextPage, EndCursor: endCursor},
		TotalCount: len(posts),
	}, nil
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
	clone := *post
	return &clone, nil
}
 
// ─── Comments ────────────────────────────────────────────────────────────────
 
func (s *MemoryStore) CreateComment(ctx context.Context, comment *model.Comment) (*model.Comment, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if len(comment.Content) > 2000 {
		return nil, &ErrCommentTooLong{MaxLength: 2000}
	}
 
	s.mu.Lock()
 
	post, ok := s.posts[comment.PostID]
	if !ok {
		s.mu.Unlock()
		return nil, &ErrPostNotFound{PostID: comment.PostID}
	}
	if !post.AllowComments {
		s.mu.Unlock()
		return nil, &ErrCommentsDisabled{PostID: comment.PostID}
	}
 
	comment.ID = generateID()
	comment.CreatedAt = time.Now()
 
	clone := *comment
	s.comments[clone.ID] = &clone
 
	if clone.ParentID == nil {
		s.postRoots[clone.PostID] = append(s.postRoots[clone.PostID], clone.ID)
	} else {
		s.byParent[*clone.ParentID] = append(s.byParent[*clone.ParentID], clone.ID)
	}
 
	// Снимаем Lock ДО публикации — иначе deadlock:
	// publishComment берёт RLock, а мы ещё держим Lock.
	subs := make([]chan *model.Comment, len(s.subscribers[clone.PostID]))
	copy(subs, s.subscribers[clone.PostID])
	s.mu.Unlock()
 
	// Публикуем уже без лока
	result := clone
	for _, ch := range subs {
		select {
		case ch <- &result:
		default: // медленный клиент — пропускаем, не блокируемся
		}
	}
 
	*comment = clone
	return &result, nil
}
 
func (s *MemoryStore) GetCommentsByPostID(ctx context.Context, postID string, first int, after *string) (*model.CommentConnection, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if first <= 0 {
		first = 10
	}
 
	s.mu.RLock()
	defer s.mu.RUnlock()
 
	rootIDs := s.postRoots[postID]
	if len(rootIDs) == 0 {
		return emptyCommentConnection(), nil
	}
	return s.paginateComments(rootIDs, first, after, true)
}
 
func (s *MemoryStore) GetRepliesByParentID(ctx context.Context, parentID string, first int, after *string) (*model.CommentConnection, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if first <= 0 {
		first = 10
	}
 
	s.mu.RLock()
	defer s.mu.RUnlock()
 
	replyIDs := s.byParent[parentID]
	if len(replyIDs) == 0 {
		return emptyCommentConnection(), nil
	}
	return s.paginateComments(replyIDs, first, after, false)
}
 
// Subscribe регистрирует подписчика и возвращает канал.
// Горутина автоматически отписывает при отмене ctx.
func (s *MemoryStore) Subscribe(ctx context.Context, postID string) (<-chan *model.Comment, error) {
	ch := make(chan *model.Comment, 16)
 
	s.mu.Lock()
	s.subscribers[postID] = append(s.subscribers[postID], ch)
	s.mu.Unlock()
 
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
 
		subs := s.subscribers[postID]
		for i, sub := range subs {
			if sub == ch {
				s.subscribers[postID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		close(ch)
	}()
 
	return ch, nil
}
 
// ─── Helpers ─────────────────────────────────────────────────────────────────
 
func generateID() string { return uuid.New().String() }
 
func checkContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
 
func emptyCommentConnection() *model.CommentConnection {
	return &model.CommentConnection{
		Edges:    []*model.CommentEdge{},
		PageInfo: &model.PageInfo{HasNextPage: false},
	}
}
 
// paginateComments — внутренний метод, вызывается под RLock.
func (s *MemoryStore) paginateComments(
	commentIDs []string,
	first int,
	after *string,
	sortNewestFirst bool,
) (*model.CommentConnection, error) {
	comments := make([]*model.Comment, 0, len(commentIDs))
	for _, id := range commentIDs {
		if c, exists := s.comments[id]; exists {
			clone := *c
			comments = append(comments, &clone)
		}
	}
 
	if len(comments) == 0 {
		return emptyCommentConnection(), nil
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
		edges[i] = &model.CommentEdge{Node: c, Cursor: c.ID}
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