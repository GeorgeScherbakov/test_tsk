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

type PostgresStore struct {
	pool *pgxpool.Pool
	connString string
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
        return nil, fmt.Errorf("failed to ping database: %w", err)
    }

    return &PostgresStore{
        pool:       pool,
        connString: connString, 
    }, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) CreatePost(ctx context.Context, post *model.Post) error {
	if post.ID == "" {
		post.ID = generateID()
	}
	post.CreatedAt = time.Now()

	query := `
		INSERT INTO posts (id, title, content, author, allow_comments, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := s.pool.Exec(ctx, query,
		post.ID,
		post.Title,
		post.Content,
		post.Author,
		post.AllowComments,
		post.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create post: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetPostByID(ctx context.Context, id string) (*model.Post, error) {
	query := `
		SELECT id, title, content, author, allow_comments, created_at
		FROM posts
		WHERE id = $1
	`

	var post model.Post
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&post.ID,
		&post.Title,
		&post.Content,
		&post.Author,
		&post.AllowComments,
		&post.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &ErrPostNotFound{PostID: id}
		}
		return nil, fmt.Errorf("failed to get post by id: %w", err)
	}

	return &post, nil
}

func (s *PostgresStore) GetPosts(ctx context.Context) ([]*model.Post, error) {
	query := `
		SELECT id, title, content, author, allow_comments, created_at
		FROM posts
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get posts: %w", err)
	}
	defer rows.Close()

	var posts []*model.Post
	for rows.Next() {
		post := &model.Post{}
		err := rows.Scan(
			&post.ID,
			&post.Title,
			&post.Content,
			&post.Author,
			&post.AllowComments,
			&post.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan post: %w", err)
		}
		posts = append(posts, post)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return posts, nil
}

func (s *PostgresStore) ToggleComments(ctx context.Context, postID string, allowComments bool) (*model.Post, error) {
	query := `
		UPDATE posts
		SET allow_comments = $1
		WHERE id = $2
		RETURNING id, title, content, author, allow_comments, created_at
	`

	var post model.Post
	err := s.pool.QueryRow(ctx, query, allowComments, postID).Scan(
		&post.ID,
		&post.Title,
		&post.Content,
		&post.Author,
		&post.AllowComments,
		&post.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &ErrPostNotFound{PostID: postID}
		}
		return nil, fmt.Errorf("failed to toggle comments: %w", err)
	}

	return &post, nil
}

func (s *PostgresStore) CreateComment(ctx context.Context, comment *model.Comment) (*model.Comment, error) {
	if comment.ID == "" {
		comment.ID = generateID()
	}
	comment.CreatedAt = time.Now()

	if len(comment.Content) > 2000 {
		return nil, &ErrCommentTooLong{MaxLength: 2000}
	}

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

	if comment.ParentID != nil {
		var parentExists bool
		err = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM comments WHERE id = $1)", *comment.ParentID).Scan(&parentExists)
		if err != nil {
			return nil, fmt.Errorf("failed to check parent comment: %w", err)
		}
		if !parentExists {
			return nil, fmt.Errorf("parent comment not found: %s", *comment.ParentID)
		}
	}

	query := `
		INSERT INTO comments (id, post_id, parent_id, author, content, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, post_id, parent_id, author, content, created_at
	`

	err = s.pool.QueryRow(ctx, query,
		comment.ID,
		comment.PostID,
		comment.ParentID,
		comment.Author,
		comment.Content,
		comment.CreatedAt,
	).Scan(
		&comment.ID,
		&comment.PostID,
		&comment.ParentID,
		&comment.Author,
		&comment.Content,
		&comment.CreatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create comment: %w", err)
	}

	return comment, nil
}

func (s *PostgresStore) GetCommentsByPostID(ctx context.Context, postID string, first int, after *string) (*model.CommentConnection, error) {
	offset := 0
	if after != nil && *after != "" {
		fmt.Sscanf(*after, "%d", &offset)
		offset++
	}

	query := `
		SELECT id, post_id, parent_id, author, content, created_at
		FROM comments
		WHERE post_id = $1 AND parent_id IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.pool.Query(ctx, query, postID, first, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get comments: %w", err)
	}
	defer rows.Close()

	var comments []*model.Comment
	for rows.Next() {
		c := &model.Comment{}
		err := rows.Scan(
			&c.ID,
			&c.PostID,
			&c.ParentID,
			&c.Author,
			&c.Content,
			&c.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan comment: %w", err)
		}
		comments = append(comments, c)
	}

	var totalCount int
	s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM comments WHERE post_id = $1 AND parent_id IS NULL", postID).Scan(&totalCount)

	edges := make([]*model.CommentEdge, len(comments))
	for i, c := range comments {
		cursor := fmt.Sprintf("%d", offset+i)
		edges[i] = &model.CommentEdge{
			Node:   c,
			Cursor: cursor,
		}
	}

	hasNextPage := offset+len(comments) < totalCount
	var endCursor *string
	if len(edges) > 0 {
		endCursor = &edges[len(edges)-1].Cursor
	}

	return &model.CommentConnection{
		Edges: edges,
		PageInfo: &model.PageInfo{
			HasNextPage: hasNextPage,
			EndCursor:   endCursor,
		},
		TotalCount: totalCount,
	}, nil
}

func (s *PostgresStore) GetRepliesByParentID(ctx context.Context, parentID string, first int, after *string) (*model.CommentConnection, error) {
	offset := 0
	if after != nil && *after != "" {
		fmt.Sscanf(*after, "%d", &offset)
		offset++
	}

	query := `
		SELECT id, post_id, parent_id, author, content, created_at
		FROM comments
		WHERE parent_id = $1
		ORDER BY created_at ASC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.pool.Query(ctx, query, parentID, first, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get replies: %w", err)
	}
	defer rows.Close()

	var comments []*model.Comment
	for rows.Next() {
		c := &model.Comment{}
		err := rows.Scan(
			&c.ID,
			&c.PostID,
			&c.ParentID,
			&c.Author,
			&c.Content,
			&c.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan reply: %w", err)
		}
		comments = append(comments, c)
	}

	var totalCount int
	s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM comments WHERE parent_id = $1", parentID).Scan(&totalCount)

	edges := make([]*model.CommentEdge, len(comments))
	for i, c := range comments {
		cursor := fmt.Sprintf("%d", offset+i)
		edges[i] = &model.CommentEdge{
			Node:   c,
			Cursor: cursor,
		}
	}

	hasNextPage := offset+len(comments) < totalCount
	var endCursor *string
	if len(edges) > 0 {
		endCursor = &edges[len(edges)-1].Cursor
	}

	return &model.CommentConnection{
		Edges: edges,
		PageInfo: &model.PageInfo{
			HasNextPage: hasNextPage,
			EndCursor:   endCursor,
		},
		TotalCount: totalCount,
	}, nil
}

func (s *PostgresStore) Subscribe(ctx context.Context, postID string) (<-chan *model.Comment, error) {
    ch := make(chan *model.Comment, 10)
    
    // Создаём отдельное соединение для LISTEN
    conn, err := pgx.Connect(ctx, s.connString)
    if err != nil {
        return nil, fmt.Errorf("failed to create listener: %w", err)
    }
    
    // Подписываемся на канал для этого поста
    channelName := fmt.Sprintf("comments_post_%s", postID)
    _, err = conn.Exec(ctx, fmt.Sprintf("LISTEN %s", channelName))
    if err != nil {
        conn.Close(ctx)
        return nil, fmt.Errorf("failed to listen: %w", err)
    }
    
    // Горутина для получения уведомлений
    go func() {
        defer conn.Close(ctx)
        
        for {
            select {
            case <-ctx.Done():
                // Отписка при завершении контекста
                conn.Exec(ctx, fmt.Sprintf("UNLISTEN %s", channelName))
                return
            default:
                // Ждём уведомление
                notification, err := conn.WaitForNotification(ctx)
                if err != nil {
                    return
                }
                
                // Парсим комментарий из JSON
                var comment model.Comment
                if err := json.Unmarshal([]byte(notification.Payload), &comment); err != nil {
                    continue
                }
                
                select {
                case ch <- &comment:
                default:
                }
            }
        }
    }()
    
    return ch, nil
}

// Unsubscribe — заглушка, канал закроется при отмене контекста
func (s *PostgresStore) Unsubscribe(ctx context.Context, postID string, ch <-chan *model.Comment) error {
    return nil
}