package repository_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"posts-service/internal/model"
	"posts-service/internal/repository"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newStore() *repository.MemoryStore {
	return repository.NewMemoryStore()
}

func mustCreatePost(t *testing.T, s *repository.MemoryStore, title, content, author string) *model.Post {
	t.Helper()
	p := &model.Post{Title: title, Content: content, Author: author}
	if err := s.CreatePost(context.Background(), p); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	return p
}

func mustCreateComment(t *testing.T, s *repository.MemoryStore, postID string, parentID *string, author, content string) *model.Comment {
	t.Helper()
	c, err := s.CreateComment(context.Background(), &model.Comment{
		PostID: postID, ParentID: parentID, Author: author, Content: content,
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	return c
}

func ptr(s string) *string { return &s }

// ─── Post: CreatePost ─────────────────────────────────────────────────────────

func TestCreatePost_SetsIDAndCreatedAt(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "Title", "Content", "Alice")

	if p.ID == "" {
		t.Error("ID should be non-empty")
	}
	if p.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
}

func TestCreatePost_AllowCommentsDefaultTrue(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	if !p.AllowComments {
		t.Error("AllowComments should default to true")
	}
}

func TestCreatePost_DuplicateIDReturnsError(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	p2 := &model.Post{ID: p.ID, Title: "T2", Content: "C", Author: "A"}
	err := s.CreatePost(context.Background(), p2)
	if err == nil {
		t.Fatal("expected ErrPostExists")
	}
	var e *repository.ErrPostExists
	if !asErr(err, &e) {
		t.Errorf("want ErrPostExists, got %T: %v", err, err)
	}
}

func TestCreatePost_CancelledContext(t *testing.T) {
	s := newStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.CreatePost(ctx, &model.Post{Title: "T", Content: "C", Author: "A"})
	if err == nil {
		t.Error("expected context error")
	}
}

// ─── Post: GetPostByID ────────────────────────────────────────────────────────

func TestGetPostByID_Found(t *testing.T) {
	s := newStore()
	created := mustCreatePost(t, s, "Hello", "World", "Bob")

	got, err := s.GetPostByID(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID || got.Title != "Hello" {
		t.Errorf("got %+v, want id=%s title=Hello", got, created.ID)
	}
}

func TestGetPostByID_NotFound(t *testing.T) {
	s := newStore()
	_, err := s.GetPostByID(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected ErrPostNotFound")
	}
	var e *repository.ErrPostNotFound
	if !asErr(err, &e) {
		t.Errorf("want ErrPostNotFound, got %T", err)
	}
}

func TestGetPostByID_ReturnsCopy(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	got, _ := s.GetPostByID(context.Background(), p.ID)
	got.Title = "MUTATED"

	got2, _ := s.GetPostByID(context.Background(), p.ID)
	if got2.Title == "MUTATED" {
		t.Error("store should return copies, not shared pointers")
	}
}

// ─── Post: GetPosts ───────────────────────────────────────────────────────────

func TestGetPosts_Empty(t *testing.T) {
	s := newStore()
	conn, err := s.GetPosts(context.Background(), 10, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if conn.TotalCount != 0 || len(conn.Edges) != 0 {
		t.Error("expected empty connection")
	}
}

func TestGetPosts_OrderedNewestFirst(t *testing.T) {
	s := newStore()
	for i := 0; i < 3; i++ {
		mustCreatePost(t, s, "T", "C", "A")
		time.Sleep(2 * time.Millisecond)
	}

	conn, _ := s.GetPosts(context.Background(), 10, nil)
	for i := 1; i < len(conn.Edges); i++ {
		if conn.Edges[i-1].Node.CreatedAt.Before(conn.Edges[i].Node.CreatedAt) {
			t.Errorf("not sorted newest-first at index %d", i)
		}
	}
}

func TestGetPosts_PaginationFirstPage(t *testing.T) {
	s := newStore()
	for i := 0; i < 5; i++ {
		mustCreatePost(t, s, "T", "C", "A")
		time.Sleep(2 * time.Millisecond)
	}

	page1, err := s.GetPosts(context.Background(), 2, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(page1.Edges) != 2 {
		t.Fatalf("want 2 edges, got %d", len(page1.Edges))
	}
	if page1.TotalCount != 5 {
		t.Errorf("want TotalCount=5, got %d", page1.TotalCount)
	}
	if !page1.PageInfo.HasNextPage {
		t.Error("want hasNextPage=true")
	}
}

func TestGetPosts_PaginationCursor(t *testing.T) {
	s := newStore()
	for i := 0; i < 5; i++ {
		mustCreatePost(t, s, "T", "C", "A")
		time.Sleep(2 * time.Millisecond)
	}

	page1, _ := s.GetPosts(context.Background(), 2, nil)
	cursor := page1.PageInfo.EndCursor

	page2, err := s.GetPosts(context.Background(), 2, cursor)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(page2.Edges) != 2 {
		t.Fatalf("want 2 edges on page2, got %d", len(page2.Edges))
	}

	// Нет пересечений
	p1IDs := map[string]bool{}
	for _, e := range page1.Edges {
		p1IDs[e.Node.ID] = true
	}
	for _, e := range page2.Edges {
		if p1IDs[e.Node.ID] {
			t.Errorf("duplicate ID %s across pages", e.Node.ID)
		}
	}
}

func TestGetPosts_LastPageHasNextFalse(t *testing.T) {
	s := newStore()
	mustCreatePost(t, s, "T", "C", "A")

	conn, _ := s.GetPosts(context.Background(), 10, nil)
	if conn.PageInfo.HasNextPage {
		t.Error("want hasNextPage=false on last page")
	}
}

// ─── Post: ToggleComments ─────────────────────────────────────────────────────

func TestToggleComments_DisableAndEnable(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	updated, err := s.ToggleComments(context.Background(), p.ID, false)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if updated.AllowComments {
		t.Error("want AllowComments=false")
	}

	// Проверяем через GetPostByID
	got, _ := s.GetPostByID(context.Background(), p.ID)
	if got.AllowComments {
		t.Error("store should reflect toggled state")
	}

	// Включаем обратно
	updated2, _ := s.ToggleComments(context.Background(), p.ID, true)
	if !updated2.AllowComments {
		t.Error("want AllowComments=true after re-enable")
	}
}

func TestToggleComments_NotFound(t *testing.T) {
	s := newStore()
	_, err := s.ToggleComments(context.Background(), "nope", false)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ─── Comment: CreateComment ───────────────────────────────────────────────────

func TestCreateComment_Success(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	c := mustCreateComment(t, s, p.ID, nil, "Bob", "Hello")

	if c.ID == "" {
		t.Error("ID should be set")
	}
	if c.PostID != p.ID {
		t.Errorf("wrong PostID: %s", c.PostID)
	}
	if c.ParentID != nil {
		t.Error("root comment should have nil ParentID")
	}
}

func TestCreateComment_TooLong(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	_, err := s.CreateComment(context.Background(), &model.Comment{
		PostID: p.ID, Author: "Bob", Content: strings.Repeat("x", 2001),
	})
	if err == nil {
		t.Fatal("expected ErrCommentTooLong")
	}
	var e *repository.ErrCommentTooLong
	if !asErr(err, &e) {
		t.Errorf("want ErrCommentTooLong, got %T", err)
	}
}

func TestCreateComment_ExactMaxLength(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	_, err := s.CreateComment(context.Background(), &model.Comment{
		PostID: p.ID, Author: "Bob", Content: strings.Repeat("x", 2000),
	})
	if err != nil {
		t.Errorf("2000 chars should be allowed: %v", err)
	}
}

func TestCreateComment_PostNotFound(t *testing.T) {
	s := newStore()
	_, err := s.CreateComment(context.Background(), &model.Comment{
		PostID: "nope", Author: "Bob", Content: "Hi",
	})
	if err == nil {
		t.Fatal("expected ErrPostNotFound")
	}
	var e *repository.ErrPostNotFound
	if !asErr(err, &e) {
		t.Errorf("want ErrPostNotFound, got %T", err)
	}
}

func TestCreateComment_CommentsDisabled(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	s.ToggleComments(context.Background(), p.ID, false)

	_, err := s.CreateComment(context.Background(), &model.Comment{
		PostID: p.ID, Author: "Bob", Content: "Hi",
	})
	if err == nil {
		t.Fatal("expected ErrCommentsDisabled")
	}
	var e *repository.ErrCommentsDisabled
	if !asErr(err, &e) {
		t.Errorf("want ErrCommentsDisabled, got %T", err)
	}
}

func TestCreateComment_WithParent(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	root := mustCreateComment(t, s, p.ID, nil, "Alice", "Root")
	reply := mustCreateComment(t, s, p.ID, &root.ID, "Bob", "Reply")

	if reply.ParentID == nil || *reply.ParentID != root.ID {
		t.Errorf("wrong ParentID: got %v, want %s", reply.ParentID, root.ID)
	}
}

func TestCreateComment_ReEnableAllowsNewComments(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	s.ToggleComments(context.Background(), p.ID, false)
	s.ToggleComments(context.Background(), p.ID, true)

	_, err := s.CreateComment(context.Background(), &model.Comment{
		PostID: p.ID, Author: "Bob", Content: "Hi",
	})
	if err != nil {
		t.Errorf("should allow comment after re-enable: %v", err)
	}
}

// ─── Comment: Pagination ──────────────────────────────────────────────────────

func TestGetCommentsByPostID_OnlyRootComments(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	root1 := mustCreateComment(t, s, p.ID, nil, "A", "Root1")
	mustCreateComment(t, s, p.ID, &root1.ID, "B", "Reply") // не должен попасть в roots

	conn, err := s.GetCommentsByPostID(context.Background(), p.ID, 10, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if conn.TotalCount != 1 {
		t.Errorf("want 1 root comment, got %d", conn.TotalCount)
	}
}

func TestGetCommentsByPostID_Pagination(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	for i := 0; i < 5; i++ {
		mustCreateComment(t, s, p.ID, nil, "A", "C")
		time.Sleep(2 * time.Millisecond)
	}

	page1, _ := s.GetCommentsByPostID(context.Background(), p.ID, 2, nil)
	if len(page1.Edges) != 2 {
		t.Fatalf("want 2, got %d", len(page1.Edges))
	}
	if !page1.PageInfo.HasNextPage {
		t.Error("want hasNextPage=true")
	}
	if page1.TotalCount != 5 {
		t.Errorf("want TotalCount=5, got %d", page1.TotalCount)
	}

	page2, _ := s.GetCommentsByPostID(context.Background(), p.ID, 2, page1.PageInfo.EndCursor)
	if len(page2.Edges) != 2 {
		t.Fatalf("want 2 on page2, got %d", len(page2.Edges))
	}

	// Нет дубликатов
	ids := map[string]bool{}
	for _, e := range page1.Edges {
		ids[e.Node.ID] = true
	}
	for _, e := range page2.Edges {
		if ids[e.Node.ID] {
			t.Errorf("duplicate %s", e.Node.ID)
		}
	}
}

func TestGetCommentsByPostID_EmptyPost(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	conn, err := s.GetCommentsByPostID(context.Background(), p.ID, 10, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(conn.Edges) != 0 || conn.TotalCount != 0 {
		t.Error("expected empty result")
	}
}

func TestGetRepliesByParentID_Pagination(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	root := mustCreateComment(t, s, p.ID, nil, "A", "Root")

	for i := 0; i < 4; i++ {
		mustCreateComment(t, s, p.ID, &root.ID, "B", "Reply")
		time.Sleep(2 * time.Millisecond)
	}

	page1, _ := s.GetRepliesByParentID(context.Background(), root.ID, 2, nil)
	if len(page1.Edges) != 2 {
		t.Fatalf("want 2, got %d", len(page1.Edges))
	}
	if !page1.PageInfo.HasNextPage {
		t.Error("want hasNextPage=true")
	}

	page2, _ := s.GetRepliesByParentID(context.Background(), root.ID, 2, page1.PageInfo.EndCursor)
	if len(page2.Edges) != 2 {
		t.Fatalf("want 2 on page2, got %d", len(page2.Edges))
	}
	if page2.PageInfo.HasNextPage {
		t.Error("want hasNextPage=false on last page")
	}
}

func TestGetRepliesByParentID_OrderOldestFirst(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")
	root := mustCreateComment(t, s, p.ID, nil, "A", "Root")

	for i := 0; i < 3; i++ {
		mustCreateComment(t, s, p.ID, &root.ID, "B", "Reply")
		time.Sleep(2 * time.Millisecond)
	}

	conn, _ := s.GetRepliesByParentID(context.Background(), root.ID, 10, nil)
	for i := 1; i < len(conn.Edges); i++ {
		if conn.Edges[i-1].Node.CreatedAt.After(conn.Edges[i].Node.CreatedAt) {
			t.Errorf("replies not sorted oldest-first at index %d", i)
		}
	}
}

// ─── Hierarchy ────────────────────────────────────────────────────────────────

func TestHierarchy_UnlimitedDepth(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	// Создаём цепочку глубиной 10
	var parentID *string
	for i := 0; i < 10; i++ {
		c := mustCreateComment(t, s, p.ID, parentID, "A", "Level")
		parentID = &c.ID
	}

	// Последний parentID — на глубине 10
	if parentID == nil {
		t.Fatal("expected non-nil parentID")
	}
	// Корневых должно быть 1
	conn, _ := s.GetCommentsByPostID(context.Background(), p.ID, 10, nil)
	if conn.TotalCount != 1 {
		t.Errorf("expected 1 root, got %d", conn.TotalCount)
	}
}

// ─── Subscriptions ────────────────────────────────────────────────────────────

func TestSubscribe_ReceivesComment(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := s.Subscribe(ctx, p.ID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	go func() {
		time.Sleep(5 * time.Millisecond)
		s.CreateComment(context.Background(), &model.Comment{
			PostID: p.ID, Author: "Bob", Content: "Hello sub",
		})
	}()

	select {
	case c := <-ch:
		if c.Content != "Hello sub" {
			t.Errorf("got %q, want 'Hello sub'", c.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: no comment received")
	}
}

func TestSubscribe_ChannelClosedOnContextCancel(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	ctx, cancel := context.WithCancel(context.Background())
	ch, _ := s.Subscribe(ctx, p.ID)

	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after ctx cancel")
	}
}

func TestSubscribe_MultipleSubscribers(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1, _ := s.Subscribe(ctx, p.ID)
	ch2, _ := s.Subscribe(ctx, p.ID)

	go func() {
		time.Sleep(5 * time.Millisecond)
		s.CreateComment(context.Background(), &model.Comment{
			PostID: p.ID, Author: "A", Content: "broadcast",
		})
	}()

	for i, ch := range []<-chan *model.Comment{ch1, ch2} {
		select {
		case c := <-ch:
			if c.Content != "broadcast" {
				t.Errorf("sub%d: got %q", i+1, c.Content)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout on subscriber %d", i+1)
		}
	}
}

func TestSubscribe_NoReceiveForOtherPost(t *testing.T) {
	s := newStore()
	p1 := mustCreatePost(t, s, "T", "C", "A")
	p2 := mustCreatePost(t, s, "T", "C", "A")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, _ := s.Subscribe(ctx, p1.ID)

	// Комментируем другой пост
	s.CreateComment(context.Background(), &model.Comment{
		PostID: p2.ID, Author: "A", Content: "other post",
	})

	select {
	case <-ch:
		t.Error("should not receive comment for different post")
	case <-time.After(100 * time.Millisecond):
		// OK
	}
}

func TestSubscribe_NoDeadlockUnderLoad(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 5 подписчиков
	for i := 0; i < 5; i++ {
		s.Subscribe(ctx, p.ID)
	}

	// 20 комментариев параллельно — не должно зависнуть
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.CreateComment(context.Background(), &model.Comment{
				PostID: p.ID, Author: "A", Content: "load",
			})
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("deadlock detected under load")
	}
}

// ─── Concurrency ─────────────────────────────────────────────────────────────

func TestConcurrency_CreatePosts(t *testing.T) {
	s := newStore()
	var wg sync.WaitGroup
	n := 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.CreatePost(context.Background(), &model.Post{Title: "T", Content: "C", Author: "A"})
		}()
	}
	wg.Wait()

	conn, _ := s.GetPosts(context.Background(), n+1, nil)
	if conn.TotalCount != n {
		t.Errorf("want %d posts, got %d", n, conn.TotalCount)
	}
}

func TestConcurrency_CreateComments(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	var wg sync.WaitGroup
	n := 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.CreateComment(context.Background(), &model.Comment{
				PostID: p.ID, Author: "A", Content: "C",
			})
		}()
	}
	wg.Wait()

	conn, _ := s.GetCommentsByPostID(context.Background(), p.ID, n+1, nil)
	if conn.TotalCount != n {
		t.Errorf("want %d comments, got %d", n, conn.TotalCount)
	}
}

func TestConcurrency_ReadWrite(t *testing.T) {
	s := newStore()
	p := mustCreatePost(t, s, "T", "C", "A")

	var wg sync.WaitGroup

	// Писатели
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.CreateComment(context.Background(), &model.Comment{
				PostID: p.ID, Author: "A", Content: "C",
			})
		}()
	}

	// Читатели
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.GetCommentsByPostID(context.Background(), p.ID, 10, nil)
			s.GetPostByID(context.Background(), p.ID)
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent read/write deadlock or timeout")
	}
}

// ─── Error types ─────────────────────────────────────────────────────────────

func TestErrorTypes_AllImplementError(t *testing.T) {
	errs := []error{
		&repository.ErrPostExists{PostID: "1"},
		&repository.ErrPostNotFound{PostID: "1"},
		&repository.ErrCommentTooLong{MaxLength: 2000},
		&repository.ErrCommentsDisabled{PostID: "1"},
	}
	for _, e := range errs {
		if e.Error() == "" {
			t.Errorf("error %T has empty message", e)
		}
	}
}

// ─── asErr helper (простой type-switch без errors.As) ────────────────────────

func asErr[T error](err error, target *T) bool {
	v, ok := err.(T)
	if ok {
		*target = v
	}
	return ok
}