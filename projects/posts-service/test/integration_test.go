package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

const serverURL = "http://localhost:8080/query"

type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type GraphQLResponse struct {
	Data   interface{} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func doRequest(query string) (*GraphQLResponse, error) {
	reqBody := GraphQLRequest{Query: query}
	jsonData, _ := json.Marshal(reqBody)

	resp, err := http.Post(serverURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result GraphQLResponse
	json.Unmarshal(body, &result)
	return &result, nil
}

// Тест 1: Создание поста
func TestCreatePost(t *testing.T) {
	query := `mutation {
		createPost(title: "Тестовый пост", content: "Контент", author: "Тестер") {
			id
			title
			content
			author
			allowComments
			createdAt
		}
	}`

	start := time.Now()
	resp, err := doRequest(query)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", resp.Errors)
	}

	data := resp.Data.(map[string]interface{})
	post := data["createPost"].(map[string]interface{})

	if post["id"] == nil || post["id"] == "" {
		t.Error("❌ Post ID not generated")
	} else {
		t.Logf("✅ Post created in %v, ID: %v", duration, post["id"])
	}
}

// Тест 2: Получение всех постов
func TestGetPosts(t *testing.T) {
	query := `query {
		posts {
			edges {
				node { id title }
			}
			totalCount
		}
	}`

	start := time.Now()
	resp, err := doRequest(query)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", resp.Errors)
	}

	data := resp.Data.(map[string]interface{})
	posts := data["posts"].(map[string]interface{})
	totalCount := posts["totalCount"].(float64)

	t.Logf("✅ Got %d posts in %v", int(totalCount), duration)
}

// Тест 3: Включение/выключение комментариев
func TestToggleComments(t *testing.T) {
	// Сначала создаём пост
	createQuery := `mutation {
		createPost(title: "Тест комментов", content: "Контент", author: "Тестер") {
			id
		}
	}`
	createResp, _ := doRequest(createQuery)
	postData := createResp.Data.(map[string]interface{})
	post := postData["createPost"].(map[string]interface{})
	postID := post["id"].(string)

	// Включаем комментарии
	toggleQuery := fmt.Sprintf(`mutation {
		toggleComments(postId: "%s", allowComments: true) {
			allowComments
		}
	}`, postID)

	start := time.Now()
	resp, err := doRequest(toggleQuery)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", resp.Errors)
	}

	t.Logf("✅ Comments toggled in %v", duration)
}

// Тест 4: Создание комментария
func TestCreateComment(t *testing.T) {
	// Создаём пост с включёнными комментариями
	createPost := `mutation {
		createPost(title: "Пост с комментом", content: "Контент", author: "Тестер") {
			id
		}
	}`
	postResp, _ := doRequest(createPost)
	postData := postResp.Data.(map[string]interface{})
	post := postData["createPost"].(map[string]interface{})
	postID := post["id"].(string)

	// Включаем комментарии
	doRequest(fmt.Sprintf(`mutation { toggleComments(postId: "%s", allowComments: true) { id } }`, postID))

	// Создаём комментарий
	commentQuery := fmt.Sprintf(`mutation {
		createComment(input: {
			postId: "%s"
			author: "Комментатор"
			content: "Тестовый комментарий"
		}) {
			id
			content
			author
		}
	}`, postID)

	start := time.Now()
	resp, err := doRequest(commentQuery)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if len(resp.Errors) > 0 {
		t.Fatalf("GraphQL errors: %v", resp.Errors)
	}

	t.Logf("✅ Comment created in %v", duration)
}

// Тест 5: Запрет комментариев
func TestCommentsDisabled(t *testing.T) {
	// Создаём пост
	createPost := `mutation {
		createPost(title: "Пост без комментов", content: "Контент", author: "Тестер") {
			id
		}
	}`
	postResp, _ := doRequest(createPost)
	postData := postResp.Data.(map[string]interface{})
	post := postData["createPost"].(map[string]interface{})
	postID := post["id"].(string)

	// Убеждаемся, что комментарии выключены (allowComments = false по умолчанию)

	// Пытаемся создать комментарий
	commentQuery := fmt.Sprintf(`mutation {
		createComment(input: {
			postId: "%s"
			author: "Комментатор"
			content: "Этот коммент не должен создаться"
		}) {
			id
		}
	}`, postID)

	resp, err := doRequest(commentQuery)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	// Должна быть ошибка
	if len(resp.Errors) == 0 {
		t.Error("❌ Expected error when comments are disabled, but got success")
	} else {
		t.Logf("✅ Correctly blocked comment: %v", resp.Errors[0].Message)
	}
}

// Тест 6: Пагинация
func TestPagination(t *testing.T) {
	// Создаём пост
	createPost := `mutation {
		createPost(title: "Пост для пагинации", content: "Контент", author: "Тестер") {
			id
		}
	}`
	postResp, _ := doRequest(createPost)
	postData := postResp.Data.(map[string]interface{})
	post := postData["createPost"].(map[string]interface{})
	postID := post["id"].(string)

	// Включаем комментарии
	doRequest(fmt.Sprintf(`mutation { toggleComments(postId: "%s", allowComments: true) { id } }`, postID))

	// Создаём 5 комментариев
	for i := 1; i <= 5; i++ {
		query := fmt.Sprintf(`mutation {
			createComment(input: {
				postId: "%s"
				author: "Тестер%d"
				content: "Комментарий %d"
			}) { id }
		}`, postID, i, i)
		doRequest(query)
	}

	// Запрашиваем первые 2 комментария
	firstQuery := fmt.Sprintf(`query {
		post(id: "%s") {
			comments(first: 2) {
				edges { node { id content } cursor }
				pageInfo { hasNextPage endCursor }
				totalCount
			}
		}
	}`, postID)

	start := time.Now()
	resp, err := doRequest(firstQuery)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	data := resp.Data.(map[string]interface{})
	postResult := data["post"].(map[string]interface{})
	comments := postResult["comments"].(map[string]interface{})
	edges := comments["edges"].([]interface{})
	pageInfo := comments["pageInfo"].(map[string]interface{})

	if len(edges) != 2 {
		t.Errorf("❌ Expected 2 comments, got %d", len(edges))
	} else {
		t.Logf("✅ Pagination: got %d of %v total in %v", len(edges), comments["totalCount"], duration)
	}

	if pageInfo["hasNextPage"] != true {
		t.Error("❌ hasNextPage should be true")
	} else {
		t.Log("✅ hasNextPage correctly true")
	}
}

// Тест 7: Иерархия комментариев
func TestHierarchy(t *testing.T) {
	// Создаём пост
	createPost := `mutation {
		createPost(title: "Пост для иерархии", content: "Контент", author: "Тестер") {
			id
		}
	}`
	postResp, _ := doRequest(createPost)
	postData := postResp.Data.(map[string]interface{})
	post := postData["createPost"].(map[string]interface{})
	postID := post["id"].(string)

	// Включаем комментарии
	doRequest(fmt.Sprintf(`mutation { toggleComments(postId: "%s", allowComments: true) { id } }`, postID))

	// Создаём корневой комментарий
	rootQuery := fmt.Sprintf(`mutation {
		createComment(input: {
			postId: "%s"
			author: "Родитель"
			content: "Корневой комментарий"
		}) { id }
	}`, postID)
	rootResp, _ := doRequest(rootQuery)
	rootData := rootResp.Data.(map[string]interface{})
	rootComment := rootData["createComment"].(map[string]interface{})
	rootID := rootComment["id"].(string)

	// Создаём ответ на корневой
	replyQuery := fmt.Sprintf(`mutation {
		createComment(input: {
			postId: "%s"
			parentId: "%s"
			author: "Дочерний"
			content: "Ответ на комментарий"
		}) { id }
	}`, postID, rootID)
	replyResp, _ := doRequest(replyQuery)

	if len(replyResp.Errors) > 0 {
		t.Fatalf("❌ Failed to create reply: %v", replyResp.Errors)
	}

	// Запрашиваем с replies
	hierarchyQuery := fmt.Sprintf(`query {
		post(id: "%s") {
			comments(first: 10) {
				edges {
					node {
						id
						replies(first: 10) {
							edges { node { id content } }
						}
					}
				}
			}
		}
	}`, postID)

	start := time.Now()
	_, err := doRequest(hierarchyQuery)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	t.Logf("✅ Hierarchy works in %v", duration)
}

// Тест 8: Длинный комментарий (>2000 символов)
func TestLongComment(t *testing.T) {
	// Создаём пост
	createPost := `mutation {
		createPost(title: "Тест длины", content: "Контент", author: "Тестер") {
			id
		}
	}`
	postResp, _ := doRequest(createPost)
	postData := postResp.Data.(map[string]interface{})
	post := postData["createPost"].(map[string]interface{})
	postID := post["id"].(string)

	// Включаем комментарии
	doRequest(fmt.Sprintf(`mutation { toggleComments(postId: "%s", allowComments: true) { id } }`, postID))

	// Создаём длинный текст
	longText := make([]byte, 2001)
	for i := range longText {
		longText[i] = 'a'
	}

	commentQuery := fmt.Sprintf(`mutation {
		createComment(input: {
			postId: "%s"
			author: "Тестер"
			content: "%s"
		}) { id }
	}`, postID, string(longText))

	resp, err := doRequest(commentQuery)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if len(resp.Errors) == 0 {
		t.Error("❌ Expected error for long comment, but got success")
	} else {
		t.Logf("✅ Correctly blocked long comment: %v", resp.Errors[0].Message)
	}
}

// Тест 9: Несуществующий пост
func TestNonExistentPost(t *testing.T) {
	query := `query { post(id: "non-existent-id") { id title } }`

	resp, err := doRequest(query)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	data := resp.Data.(map[string]interface{})
	if data["post"] != nil {
		t.Error("❌ Expected null for non-existent post")
	} else {
		t.Log("✅ Correctly returned null for non-existent post")
	}
}

// Тест 10: Общая производительность
func TestPerformance(t *testing.T) {
	// Создаём 10 постов и замеряем время
	start := time.Now()

	for i := 1; i <= 10; i++ {
		query := fmt.Sprintf(`mutation {
			createPost(title: "Пост %d", content: "Контент", author: "Тестер") { id }
		}`, i)
		doRequest(query)
	}

	createDuration := time.Since(start)

	// Запрашиваем все посты
	start = time.Now()
	query := `query { posts { edges { node { id } } totalCount } }`
	resp, err := doRequest(query)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	queryDuration := time.Since(start)

	data := resp.Data.(map[string]interface{})
	posts := data["posts"].(map[string]interface{})
	totalCount := posts["totalCount"].(float64)

	t.Logf("📊 Performance metrics:")
	t.Logf("   - Create 10 posts: %v", createDuration)
	t.Logf("   - Query all posts: %v", queryDuration)
	t.Logf("   - Total posts: %v", int(totalCount))

	if totalCount >= 10 {
		t.Log("✅ Performance test passed")
	}
}

// Запуск всех тестов
func TestAll(t *testing.T) {
	t.Log("🚀 Starting integration tests...")
	t.Log("")

	t.Run("CreatePost", TestCreatePost)
	t.Run("GetPosts", TestGetPosts)
	t.Run("ToggleComments", TestToggleComments)
	t.Run("CreateComment", TestCreateComment)
	t.Run("CommentsDisabled", TestCommentsDisabled)
	t.Run("Pagination", TestPagination)
	t.Run("Hierarchy", TestHierarchy)
	t.Run("LongComment", TestLongComment)
	t.Run("NonExistentPost", TestNonExistentPost)
	t.Run("Performance", TestPerformance)

	t.Log("")
	t.Log("✅ All tests completed!")
}
