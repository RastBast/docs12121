package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const workflowTemplate = `name: OpenAPI Docs Aggregator
run-name: Aggregating OpenAPI docs from ${{ gitea.repository }}

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
          BRANCH_NAME=$(echo "${{ gitea.ref }}" | sed 's/refs\/heads\///')
          echo "repo_name=$REPO_NAME" >> $GITHUB_OUTPUT
          echo "branch_name=$BRANCH_NAME" >> $GITHUB_OUTPUT

      - name: Validate OpenAPI file
        run: |
          npm install -g swagger-parser
          swagger-parser validate docs/openapi.yaml

      - name: Check for breaking changes
        if: github.ref != 'refs/heads/main'
        run: |
          curl -sSL https://github.com/Tufin/oasdiff/releases/latest/download/oasdiff.linux.amd64 -o oasdiff
          chmod +x oasdiff
          oasdiff breaking docs-repo/${{ steps.repo_info.outputs.repo_name }}/openapi.yaml docs/openapi.yaml

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

      - name: Generate static HTML
        run: |
          npx @openapitools/openapi-generator-cli generate -i docs/openapi.yaml -g html2 -o docs-repo/static/${{ steps.repo_info.outputs.repo_name }}
          mkdir -p docs-repo/interactive/${{ steps.repo_info.outputs.repo_name }}
          cp -r /usr/local/lib/node_modules/swagger-ui-dist/* docs-repo/interactive/${{ steps.repo_info.outputs.repo_name }}/
          sed -i 's|https://petstore.swagger.io/v2/swagger.json|../openapi.yaml|g' docs-repo/interactive/${{ steps.repo_info.outputs.repo_name }}/index.html

      - name: Generate changelog
        run: |
          github_changelog_generator --user ${{ github.repository_owner }} --project ${{ steps.repo_info.outputs.repo_name }} --output docs-repo/${{ steps.repo_info.outputs.repo_name }}/CHANGELOG.md --since-tag v1.0.0

      - name: Update portal index
        run: |
          cat >> docs-repo/index.html << EOF
          <div class="api-card">
            <h3>${{ steps.repo_info.outputs.repo_name }}</h3>
            <p>Updated: $(date)</p>
            <a href="./interactive/${{ steps.repo_info.outputs.repo_name }}/index.html">Interactive</a>
            <a href="./static/${{ steps.repo_info.outputs.repo_name }}/index.html">Static</a>
          </div>
          EOF

      - name: Collect metrics
        run: |
          curl -X POST "https://metrics.yourcompany.com/api/docs-update" \
            -H "Content-Type: application/json" \
            -d '{
              "repository": "${{ github.repository }}",
              "branch": "${{ steps.repo_info.outputs.branch_name }}",
              "timestamp": "${{ github.event.head_commit.timestamp }}",
              "file_size": $(stat -c%s docs/openapi.yaml)
            }'

      - name: Commit and push changes
        run: |
          cd docs-repo
          git config user.name "OpenAPI Aggregator Bot"
          git config user.email "openapi-bot@%s"
          git add .
          if git diff --staged --quiet; then
            echo "No changes"
          else
            git commit -m "Update docs for ${{ steps.repo_info.outputs.repo_name }} on ${{ steps.repo_info.outputs.branch_name }}"
            git push origin ${{ steps.repo_info.outputs.branch_name }}
          fi

      - name: Notify Slack
        if: always()
        uses: slackapi/slack-github-action@v1.24.0
        with:
          channel-id: 'CHANNEL_ID'
          payload: |
            { "text": "Docs updated for ${{ steps.repo_info.outputs.repo_name }}", "blocks": [...] }
        env:
          SLACK_WEBHOOK_URL: ${{ secrets.SLACK_WEBHOOK_URL }}

      - name: Cache npm
        uses: actions/cache@v3
        with:
          path: ~/.npm
          key: ${{ runner.os }}-node-${{ hashFiles('**/package-lock.json') }}
          restore-keys: |
            ${{ runner.os }}-node-
`

type Config struct {
	GiteaHost    string
	Organization string
	Repositories []string
	DocsRepo     string
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Использование: go run main.go <generate|setup>")
	}
	switch os.Args[1] {
	case "generate":
		generateWorkflow()
	case "setup":
		setupProject()
	default:
		log.Fatal("Команда не распознана")
	}
}

func getConfig() Config {
	return Config{
		GiteaHost:    getEnv("GITEA_HOST", "gitea.example.com"),
		Organization: getEnv("ORGANIZATION", "myorg"),
		DocsRepo:     getEnv("DOCS_REPO", "docs"),
		Repositories: strings.Split(getEnv("REPOSITORIES", "repo1,repo2,repo3"), ","),
	}
}

func generateWorkflow() {
	cfg := getConfig()
	dir := ".gitea/workflows"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	content := fmt.Sprintf(workflowTemplate,
		cfg.Organization, cfg.GiteaHost, cfg.Organization, cfg.GiteaHost,
	)
	path := filepath.Join(dir, "openapi-aggregator.yml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println("✅ Workflow generated at", path)
}

func setupProject() {
	fmt.Println("🚀 Setup configuration")
	cfg := interactiveConfig()
	env := fmt.Sprintf(
		"GITEA_HOST=%s\nORGANIZATION=%s\nDOCS_REPO=%s\nREPOSITORIES=%s\n",
		cfg.GiteaHost, cfg.Organization, cfg.DocsRepo, strings.Join(cfg.Repositories, ","),
	)
	if err := os.WriteFile(".env", []byte(env), 0o644); err != nil {
		log.Fatal(err)
	}
	generateWorkflow()
}

func interactiveConfig() Config {
	var cfg Config
	fmt.Print("Gitea Host: ")
	fmt.Scanln(&cfg.GiteaHost)
	fmt.Print("Organization: ")
	fmt.Scanln(&cfg.Organization)
	fmt.Print("Docs Repo (default 'docs'): ")
	fmt.Scanln(&cfg.DocsRepo)
	if cfg.DocsRepo == "" {
		cfg.DocsRepo = "docs"
	}
	fmt.Print("Repositories (comma-separated): ")
	var repos string
	fmt.Scanln(&repos)
	cfg.Repositories = strings.Split(repos, ",")
	return cfg
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
