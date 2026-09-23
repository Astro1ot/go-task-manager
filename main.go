package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Task struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IsCompleted bool   `json:"is_completed"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://task_user:task_password@localhost:5432/task_manager?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("настройка подключения к БД: %w", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		return fmt.Errorf("подключение к PostgreSQL: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		return fmt.Errorf("создание таблицы tasks: %w", err)
	}

	return newRouter(db).Run(":8080")
}

func migrate(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS tasks (
			id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			title TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			is_completed BOOLEAN NOT NULL DEFAULT FALSE
		)
	`)
	return err
}

func newRouter(db *pgxpool.Pool) *gin.Engine {
	router := gin.Default()

	router.GET("/tasks", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		rows, err := db.Query(ctx, `
			SELECT id, title, description, is_completed
			FROM tasks ORDER BY id
		`)
		if err != nil {
			databaseError(c, err)
			return
		}
		defer rows.Close()

		tasks := []Task{}
		for rows.Next() {
			var task Task
			if err := rows.Scan(&task.ID, &task.Title, &task.Description, &task.IsCompleted); err != nil {
				databaseError(c, err)
				return
			}
			tasks = append(tasks, task)
		}
		if err := rows.Err(); err != nil {
			databaseError(c, err)
			return
		}

		c.JSON(http.StatusOK, tasks)
	})

	router.POST("/tasks", func(c *gin.Context) {
		var task Task
		if err := c.ShouldBindJSON(&task); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Некорректный JSON"})
			return
		}
		task.Title = strings.TrimSpace(task.Title)
		if task.Title == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Название задачи обязательно"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		err := db.QueryRow(ctx, `
			INSERT INTO tasks (title, description, is_completed)
			VALUES ($1, $2, $3)
			RETURNING id, title, description, is_completed
		`, task.Title, task.Description, task.IsCompleted).
			Scan(&task.ID, &task.Title, &task.Description, &task.IsCompleted)
		if err != nil {
			databaseError(c, err)
			return
		}

		c.JSON(http.StatusCreated, task)
	})

	router.DELETE("/tasks/:id", func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 32)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ID должен быть положительным целым числом в диапазоне INTEGER"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		result, err := db.Exec(ctx, "DELETE FROM tasks WHERE id = $1", id)
		if err != nil {
			databaseError(c, err)
			return
		}
		if result.RowsAffected() == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "Задача не найдена"})
			return
		}

		c.Status(http.StatusNoContent)
	})

	return router
}

func databaseError(c *gin.Context, err error) {
	log.Printf("Ошибка БД: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "Ошибка при работе с базой данных"})
}
