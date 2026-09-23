package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func requestTaskAPI(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestRejectInvalidTaskRequests(t *testing.T) {
	// Некорректные запросы должны отклоняться до обращения к БД.
	router := newRouter(nil)
	cases := []struct {
		name, method, path, body string
	}{
		{"malformed JSON", http.MethodPost, "/tasks", `{"title":`},
		{"wrong field type", http.MethodPost, "/tasks", `{"title":123}`},
		{"missing title", http.MethodPost, "/tasks", `{}`},
		{"blank title", http.MethodPost, "/tasks", `{"title":"  "}`},
		{"null body", http.MethodPost, "/tasks", `null`},
		{"nonnumeric ID", http.MethodDelete, "/tasks/abc", ""},
		{"zero ID", http.MethodDelete, "/tasks/0", ""},
		{"negative ID", http.MethodDelete, "/tasks/-1", ""},
		{"overflow ID", http.MethodDelete, "/tasks/2147483648", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := requestTaskAPI(router, tc.method, tc.path, tc.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
			}
			var result map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result["error"] == "" {
				t.Fatalf("expected JSON error, got %s", response.Body.String())
			}
		})
	}
}

func TestTaskPersistence(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	// Отдельная схема сохраняет существующие задачи пользователя нетронутыми.
	schema := pgx.Identifier{fmt.Sprintf("tasks_test_%d", time.Now().UnixNano())}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("remove test schema: %v", err)
		}
	}()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, config.Copy())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	router := newRouter(db)

	response := requestTaskAPI(router, http.MethodGet, "/tasks", "")
	if response.Code != http.StatusOK || response.Body.String() != "[]" {
		t.Fatalf("empty GET: %d %s", response.Code, response.Body.String())
	}
	response = requestTaskAPI(router, http.MethodPost, "/tasks",
		`{"id":999,"title":"купить молоко","description":"НЕ ЗАБЫТЬ: O'Reilly","is_completed":true}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", response.Code, response.Body.String())
	}
	var created Task
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID != 1 || created.Title != "купить молоко" || created.Description != "НЕ ЗАБЫТЬ: O'Reilly" || !created.IsCompleted {
		t.Fatalf("unexpected created task: %+v", created)
	}

	// Закрываем все подключения и повторяем миграцию, как при перезапуске.
	db.Close()
	reconnected, err := pgxpool.NewWithConfig(ctx, config.Copy())
	if err != nil {
		t.Fatal(err)
	}
	db = reconnected
	if err := migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	router = newRouter(db)
	response = requestTaskAPI(router, http.MethodGet, "/tasks", "")
	var saved []Task
	if response.Code != http.StatusOK {
		t.Fatalf("GET after reconnect: %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0] != created {
		t.Fatalf("task not preserved after reconnect: %+v", saved)
	}

	path := "/tasks/" + strconv.Itoa(created.ID)
	response = requestTaskAPI(router, http.MethodDelete, path, "")
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("DELETE: %d %s", response.Code, response.Body.String())
	}
	response = requestTaskAPI(router, http.MethodDelete, path, "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("repeated DELETE: %d %s", response.Code, response.Body.String())
	}
	response = requestTaskAPI(router, http.MethodGet, "/tasks", "")
	if response.Code != http.StatusOK || response.Body.String() != "[]" {
		t.Fatalf("GET after DELETE: %d %s", response.Code, response.Body.String())
	}
	response = requestTaskAPI(router, http.MethodPost, "/tasks", `{"title":"Следующая задача"}`)
	var next Task
	if err := json.Unmarshal(response.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusCreated || next.ID <= created.ID || next.IsCompleted || next.Description != "" {
		t.Fatalf("POST after DELETE: %d %+v", response.Code, next)
	}
}
