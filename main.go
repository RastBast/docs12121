package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const workflowTemplate = `name: OpenAPI Docs Aggregator
run: Aggregating OpenAPI docs from ${{ gitea.repository }}

on:
  push:
    branches:
      - main
      - staging
      - dev
    paths:
      - 'docs/openapi.yaml'

jobs:
  aggregate-openapi:
    runs-on: ubuntu-latest
    if: ${{ gitea.repository != '%s/docs' }}
    
    steps:
      - name: Checkout source repository
        uses: actions/checkout@v4
        with:
          token: ${{ secrets.GITEA_TOKEN }}
      - name: Extract repository info
        id: repo_info
        run: |
          REPO_NAME=$(echo "${{ gitea.repository }}" | cut -d'/' -f2)
          BRANCH_NAME=$(echo "${{ gitea.ref }}" | sed 's|refs/heads/||')
          echo "repo_name=$REPO_NAME" >> $GITHUB_OUTPUT
          echo "branch_name=$BRANCH_NAME" >> $GITHUB_OUTPUT
      - name: Check if OpenAPI file exists
        id: check_file
        run: |
          if [ -f "docs/openapi.yaml" ]; then
            echo "file_exists=true" >> $GITHUB_OUTPUT
          else
            echo "file_exists=false" >> $GITHUB_OUTPUT
            echo "OpenAPI file not found in docs/openapi.yaml"
            exit 1
          fi
      - name: Clone docs repository
        run: |
          git clone https://${{ secrets.GITEA_TOKEN }}@%s/%s/docs.git docs-repo
          cd docs-repo
          if git show-branch remotes/origin/${{ steps.repo_info.outputs.branch_name }} 2>/dev/null; then
            git checkout ${{ steps.repo_info.outputs.branch_name }}
          else
            git checkout -b ${{ steps.repo_info.outputs.branch_name }}
          fi
      - name: Copy OpenAPI file
        run: |
          mkdir -p docs-repo/${{ steps.repo_info.outputs.repo_name }}
          cp docs/openapi.yaml docs-repo/${{ steps.repo_info.outputs.repo_name }}/openapi.yaml
      - name: Commit and push changes
        run: |
          cd docs-repo
          git config user.name "OpenAPI Aggregator Bot"
          git config user.email "openapi-bot@%s"
          git add ${{ steps.repo_info.outputs.repo_name }}/openapi.yaml
          if git diff --staged --quiet; then
            echo "No changes to commit"
          else
            git commit -m "Update OpenAPI docs for ${{ steps.repo_info.outputs.repo_name }} from branch ${{ steps.repo_info.outputs.branch_name }}"
            git push origin ${{ steps.repo_info.outputs.branch_name }}
          fi
`

type Config struct {
	GiteaHost    string
	Organization string
	Repositories []string
	DocsRepo     string
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Использование: go run main.go <команда>\nКоманды: generate, setup")
	}

	switch os.Args[1] {
	case "generate":
		generateWorkflows()
	case "setup":
		setupProject()
	default:
		log.Fatal("Неизвестная команда. Доступные команды: generate, setup")
	}
}

func generateWorkflows() {
	cfg := getConfig()

	workflowDir := ".gitea/workflows"
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		log.Fatalf("Ошибка создания директории: %v", err)
	}

	content := fmt.Sprintf(workflowTemplate,
		cfg.Organization,
		cfg.GiteaHost,
		cfg.Organization,
		cfg.GiteaHost,
	)

	path := filepath.Join(workflowDir, "openapi-aggregator.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		log.Fatalf("Ошибка записи файла: %v", err)
	}

	fmt.Printf("✅ Воркфлоу создан: %s\n", path)
	createReadme(cfg)
}

func setupProject() {
	fmt.Println("🚀 Настройка проекта агрегатора OpenAPI документации")

	cfg := getConfigInteractive()

	env := fmt.Sprintf(`GITEA_HOST=%s
ORGANIZATION=%s
DOCS_REPO=%s
REPOSITORIES=%s
`,
		cfg.GiteaHost,
		cfg.Organization,
		cfg.DocsRepo,
		strings.Join(cfg.Repositories, ","),
	)
	if err := os.WriteFile(".env", []byte(env), 0o644); err != nil {
		log.Fatalf("Ошибка создания .env: %v", err)
	}
	fmt.Println("✅ Конфигурация сохранена в .env")

	generateWorkflows()
}

func getConfig() Config {
	return Config{
		GiteaHost:    getEnvOrDefault("GITEA_HOST", "gitea.example.com"),
		Organization: getEnvOrDefault("ORGANIZATION", "myorg"),
		DocsRepo:     getEnvOrDefault("DOCS_REPO", "docs"),
		Repositories: strings.Split(getEnvOrDefault("REPOSITORIES", "repo1,repo2,repo3"), ","),
	}
}

func getConfigInteractive() Config {
	var cfg Config
	fmt.Print("Хост Gitea: ")
	fmt.Scanln(&cfg.GiteaHost)
	fmt.Print("Организация: ")
	fmt.Scanln(&cfg.Organization)
	fmt.Print("Репозиторий для документации (по умолчанию 'docs'): ")
	fmt.Scanln(&cfg.DocsRepo)
	if cfg.DocsRepo == "" {
		cfg.DocsRepo = "docs"
	}
	fmt.Print("Репозитории через запятую: ")
	var repos string
	fmt.Scanln(&repos)
	cfg.Repositories = strings.Split(repos, ",")
	return cfg
}

func createReadme(cfg Config) {
	content := fmt.Sprintf(`# OpenAPI Documentation Aggregator

Этот проект автоматически собирает OpenAPI документацию из разных репозиториев.

## Конфигурация
- Gitea Host: %s
- Организация: %s
- Репозиторий документации: %s
- Отслеживаемые репозитории: %s

## Как это работает
1. Пуш в ветки main, staging или dev.
2. Проверка docs/openapi.yaml.
3. Копирование файла в репозиторий документации.
4. Коммит и пуш.

## Структура результата
%s/
├── %s/
│   └── openapi.yaml
├── %s/
│   └── openapi.yaml
└── %s/
    └── openapi.yaml
`,
		cfg.GiteaHost,
		cfg.Organization,
		cfg.DocsRepo,
		strings.Join(cfg.Repositories, ", "),
		cfg.DocsRepo,
		cfg.Repositories[0],
		cfg.Repositories[1],
		cfg.Repositories[2],
	)
	if err := os.WriteFile("README.md", []byte(content), 0o644); err != nil {
		log.Printf("Не удалось создать README.md: %v", err)
	} else {
		fmt.Println("✅ README.md создан")
	}
}

func getEnvOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
