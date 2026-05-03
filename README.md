# PR Reviewer AI

PR Reviewer AI is an automated code review agent that integrates with GitLab. It uses Large Language Models (LLMs) to provide insightful feedback on Merge Requests, helping teams maintain code quality and security without manual overhead.

## Key Features

- **AI-Powered Reviews**: Connects to multiple LLM providers (Gemini, Groq, DeepSeek, Mistral, Cerebras) with automatic failover.
- **Secure by Design**: Encrypts Personal Access Tokens and GitLab URLs at rest using AES-256-GCM.
- **High Performance**: Uses Redis-backed session caching and sliding-window rate limiting.
- **Project Agnostic**: List all your accessible projects and select a default project for reviews.
- **Audit Trail**: Keeps a permanent log of all reviews posted for future reference.

## Tech Stack

- **Backend**: Go 1.24+ (Gin Framework)
- **Database**: PostgreSQL (with [Ent](https://entgo.io/) ORM)
- **Cache**: Redis (Sessions & Rate Limiting)
- **Infrastructure**: Docker & Docker Compose

---

## Getting Started

### 1. Prerequisites
- [Docker](https://www.docker.com/) and Docker Compose installed.
- A GitLab Personal Access Token (PAT) with `api` scope.
- An API key from an LLM provider (e.g., [Google AI Studio](https://aistudio.google.com/) for Gemini).

### 2. Configuration
Copy the example environment file and fill in your secrets:
```bash
cp .env.example .env
```
Key variables to set:
- `ENCRYPTION_KEY`: 64-char hex string for AES encryption.
- `JWT_SECRET`: Random string for signing session tokens.
- `GEMINI_API_KEY`: Your LLM provider key.
- `GITLAB_BASE_URL`: Your GitLab instance URL.

### 3. Running the App
Start the entire stack using Docker Compose:
```bash
docker-compose up --build
```
The API will be available at `http://localhost:8080`.

---

## API Workflow

### 1. Registration
Register your account with your GitLab credentials. This validates your token and stores your default instance URL.
```bash
POST /api/auth/register
{
  "username": "your_user",
  "password": "your_password",
  "token": "glpat-XXXX",
  "webUrl": "https://gitlab.com"
}
```

### 2. Login
Login to receive a JWT session token. This warms the Redis cache with your encrypted credentials.
```bash
POST /api/auth/login
{
  "username": "your_user",
  "password": "your_password"
}
```

### 3. Select a Project
List your accessible projects and set your default project ID for future reviews.
```bash
GET /api/projects  # Returns list of {id, name}
PUT /api/project   # Set your default
{
  "project_id": 12345
}
```

### 4. Trigger a Review
Post an AI review to a specific Merge Request.
```bash
POST /api/review
{
  "mr_id": 42
}
```

### 5. Background Monitoring (Automatic Reviews)
The project includes a background worker that polls GitLab events for all registered users every minute. 
- It automatically detects new Merge Requests and updates.
- It triggers an AI review without any manual API call.
- It tracks a `last_event_id` per user to ensure each event is processed exactly once.

---

## Development

- **Database**: The app uses Ent for schema management. If you modify schemas in `ent/schema/`, run `go generate ./ent`.
- **Migrations**: Core table structures are defined in `db/migrations/001_schema.sql`.
- **Rate Limiting**: Auth routes are limited to 5 req/15min. API routes are limited to 100 req/min.
