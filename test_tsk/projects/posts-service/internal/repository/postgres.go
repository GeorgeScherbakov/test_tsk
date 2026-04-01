package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"posts-service/internal/model"

	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

// PostgresStore реализует PostRepository и CommentRepository.
// Подписки работают через LISTEN/NOTIFY PostgreSQL.
type PostgresStore struct {
	pool       *pgxpool.Pool
	connString string // нужен только для Subscribe (отдельное соединение)
}

func NewPostgresStore(connString string) (*PostgresStore, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	pool, err := pgxpool.ConnectConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresStore{pool: pool, connString: connString}, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

// ─── Posts ───────────────────────────────────────────────────────────────────

func (s *PostgresStore) CreatePost(ctx context.Context, post *model.Post) error {
	if post.ID == "" {
		post.ID = generateID()
	}
	post.AllowComments = true
	post.CreatedAt = time.Now()

	const q = `
		INSERT INTO posts (id, title, content, author, allow_comments, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err := s.pool.Exec(ctx, q,
		post.ID, post.Title, post.Content, post.Author, post.AllowComments, post.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create post: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetPostByID(ctx context.Context, id string) (*model.Post, error) {
	const q = `SELECT id, title, content, author, allow_comments, created_at FROM posts WHERE id = $1`

	var p model.Post
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&p.ID, &p.Title, &p.Content, &p.Author, &p.AllowComments, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &ErrPostNotFound{PostID: id}
		}
		return nil, fmt.Errorf("failed to get post: %w", err)
	}
	return &p, nil
}

// GetPosts — keyset-пагинация: cursor = ID поста (UUID).
// Порядок: newest first (created_at DESC).
func (s *PostgresStore) GetPosts(ctx context.Context, first int, after *string) (*model.PostConnection, error) {
	if first <= 0 {
		first = 10
	}

	var totalCount int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM posts").Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("failed to count posts: %w", err)
	}

	// Запрашиваем first+1, чтобы определить hasNextPage без отдельного COUNT
	var rows pgx.Rows
	var err error

	if after == nil || *after == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, title, content, author, allow_comments, created_at
			 FROM posts ORDER BY created_at DESC, id DESC LIMIT $1`,
			first+1)
	} else {
		// Keyset: берём записи, которые "старше" курсора
		rows, err = s.pool.Query(ctx,
			`SELECT id, title, content, author, allow_comments, created_at
			 FROM posts
			 WHERE (created_at, id) < (
			     SELECT created_at, id FROM posts WHERE id = $1
			 )
			 ORDER BY created_at DESC, id DESC LIMIT $2`,
			*after, first+1)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get posts: %w", err)
	}
	defer rows.Close()

	var posts []*model.Post
	for rows.Next() {
		p := &model.Post{}
		if err := rows.Scan(&p.ID, &p.Title, &p.Content, &p.Author, &p.AllowComments, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan post: %w", err)
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	hasNextPage := len(posts) > first
	if hasNextPage {
		posts = posts[:first]
	}

	edges := make([]*model.PostEdge, len(posts))
	for i, p := range posts {
		edges[i] = &model.PostEdge{Node: p, Cursor: p.ID}
	}

	var endCursor *string
	if len(edges) > 0 {
		endCursor = &edges[len(edges)-1].Cursor
	}

	return &model.PostConnection{
		Edges:      edges,
		PageInfo:   &model.PageInfo{HasNextPage: hasNextPage, EndCursor: endCursor},
		TotalCount: totalCount,
	}, nil
}

func (s *PostgresStore) ToggleComments(ctx context.Context, postID string, allowComments bool) (*model.Post, error) {
	const q = `
		UPDATE posts SET allow_comments = $1 WHERE id = $2
		RETURNING id, title, content, author, allow_comments, created_at`

	var p model.Post
	err := s.pool.QueryRow(ctx, q, allowComments, postID).Scan(
		&p.ID, &p.Title, &p.Content, &p.Author, &p.AllowComments, &p.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &ErrPostNotFound{PostID: postID}
		}
		return nil, fmt.Errorf("failed to toggle comments: %w", err)
	}
	return &p, nil
}

// ─── Comments ────────────────────────────────────────────────────────────────

func (s *PostgresStore) CreateComment(ctx context.Context, comment *model.Comment) (*model.Comment, error) {
	if len(comment.Content) > 2000 {
		return nil, &ErrCommentTooLong{MaxLength: 2000}
	}

	// Проверяем пост и allow_comments
	var allowComments bool
	err := s.pool.QueryRow(ctx, "SELECT allow_comments FROM posts WHERE id = $1", comment.PostID).Scan(&allowComments)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &ErrPostNotFound{PostID: comment.PostID}
		}
		return nil, fmt.Errorf("failed to check post: %w", err)
	}
	if !allowComments {
		return nil, &ErrCommentsDisabled{PostID: comment.PostID}
	}

	// Проверяем parent
	if comment.ParentID != nil {
		var exists bool
		if err := s.pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM comments WHERE id = $1)", *comment.ParentID,
		).Scan(&exists); err != nil {
			return nil, fmt.Errorf("failed to check parent: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("parent comment not found: %s", *comment.ParentID)
		}
	}

	if comment.ID == "" {
		comment.ID = generateID()
	}
	comment.CreatedAt = time.Now()

	const q = `
		INSERT INTO comments (id, post_id, parent_id, author, content, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, post_id, parent_id, author, content, created_at`

	result := &model.Comment{}
	err = s.pool.QueryRow(ctx, q,
		comment.ID, comment.PostID, comment.ParentID, comment.Author, comment.Content, comment.CreatedAt,
	).Scan(&result.ID, &result.PostID, &result.ParentID, &result.Author, &result.Content, &result.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to create comment: %w", err)
	}

	// NOTIFY — не блокируем возврат
	go s.notify(result)

	return result, nil
}

// notify отправляет NOTIFY в PostgreSQL для подписчиков.
func (s *PostgresStore) notify(comment *model.Comment) {
	payload, err := json.Marshal(comment)
	if err != nil {
		return
	}
	channelName := "post_" + comment.PostID

	ctx := context.Background()
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	// pgx не поддерживает параметры в NOTIFY — экранируем вручную
	safePayload := string(payload)
	_, _ = conn.Exec(ctx, fmt.Sprintf("SELECT pg_notify('%s', $1)", channelName), safePayload)
}

// GetCommentsByPostID — keyset-пагинация корневых комментариев (newest first).
// cursor = UUID последнего комментария на странице.
func (s *PostgresStore) GetCommentsByPostID(ctx context.Context, postID string, first int, after *string) (*model.CommentConnection, error) {
	if first <= 0 {
		first = 10
	}

	var totalCount int
	s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM comments WHERE post_id = $1 AND parent_id IS NULL", postID,
	).Scan(&totalCount)

	var rows pgx.Rows
	var err error

	if after == nil || *after == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, post_id, parent_id, author, content, created_at
			 FROM comments
			 WHERE post_id = $1 AND parent_id IS NULL
			 ORDER BY created_at DESC, id DESC LIMIT $2`,
			postID, first+1)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, post_id, parent_id, author, content, created_at
			 FROM comments
			 WHERE post_id = $1 AND parent_id IS NULL
			   AND (created_at, id) < (
			       SELECT created_at, id FROM comments WHERE id = $2
			   )
			 ORDER BY created_at DESC, id DESC LIMIT $3`,
			postID, *after, first+1)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get comments: %w", err)
	}
	defer rows.Close()

	return s.scanCommentConnection(rows, first, totalCount)
}

// GetRepliesByParentID — keyset-пагинация ответов (oldest first, хронологически).
func (s *PostgresStore) GetRepliesByParentID(ctx context.Context, parentID string, first int, after *string) (*model.CommentConnection, error) {
	if first <= 0 {
		first = 10
	}

	var totalCount int
	s.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM comments WHERE parent_id = $1", parentID,
	).Scan(&totalCount)

	var rows pgx.Rows
	var err error

	if after == nil || *after == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, post_id, parent_id, author, content, created_at
			 FROM comments WHERE parent_id = $1
			 ORDER BY created_at ASC, id ASC LIMIT $2`,
			parentID, first+1)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, post_id, parent_id, author, content, created_at
			 FROM comments
			 WHERE parent_id = $1
			   AND (created_at, id) > (
			       SELECT created_at, id FROM comments WHERE id = $2
			   )
			 ORDER BY created_at ASC, id ASC LIMIT $3`,
			parentID, *after, first+1)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get replies: %w", err)
	}
	defer rows.Close()

	return s.scanCommentConnection(rows, first, totalCount)
}

// Subscribe слушает NOTIFY от PostgreSQL в отдельном соединении.
// Канал закрывается при отмене ctx.
func (s *PostgresStore) Subscribe(ctx context.Context, postID string) (<-chan *model.Comment, error) {
	ch := make(chan *model.Comment, 16)

	conn, err := pgx.Connect(ctx, s.connString)
	if err != nil {
		return nil, fmt.Errorf("failed to create listener connection: %w", err)
	}

	channelName := "post_" + postID
	if _, err := conn.Exec(ctx, "LISTEN "+channelName); err != nil {
		conn.Close(ctx)
		return nil, fmt.Errorf("failed to LISTEN: %w", err)
	}

	go func() {
		defer conn.Close(context.Background())
		defer close(ch)

		for {
			notification, err := conn.WaitForNotification(ctx)
			if err != nil {
				// ctx отменён или соединение разорвано — выходим
				return
			}

			var comment model.Comment
			if err := json.Unmarshal([]byte(notification.Payload), &comment); err != nil {
				continue
			}

			select {
			case ch <- &comment:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (s *PostgresStore) scanCommentConnection(rows pgx.Rows, first, totalCount int) (*model.CommentConnection, error) {
	var comments []*model.Comment
	for rows.Next() {
		c := &model.Comment{}
		if err := rows.Scan(&c.ID, &c.PostID, &c.ParentID, &c.Author, &c.Content, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan comment: %w", err)
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	hasNextPage := len(comments) > first
	if hasNextPage {
		comments = comments[:first]
	}

	edges := make([]*model.CommentEdge, len(comments))
	for i, c := range comments {
		edges[i] = &model.CommentEdge{Node: c, Cursor: c.ID}
	}

	var endCursor *string
	if len(edges) > 0 {
		endCursor = &edges[len(edges)-1].Cursor
	}

	return &model.CommentConnection{
		Edges:      edges,
		PageInfo:   &model.PageInfo{HasNextPage: hasNextPage, EndCursor: endCursor},
		TotalCount: totalCount,
	}, nil
}