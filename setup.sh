#!/bin/bash

# Configuration
BASE_URL="http://localhost:8080"

# Colors for better UI
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}==========================================${NC}"
echo -e "${BLUE}      PR Reviewer AI - CLI Setup         ${NC}"
echo -e "${BLUE}==========================================${NC}"

# 1. Login
echo -e "${YELLOW}Please Login${NC}"
read -p "Username: " USERNAME
read -sp "Password: " PASSWORD
echo ""

echo -e "\n${BLUE}[*] Logging in...${NC}"

LOGIN_RESPONSE=$(curl -s -X POST "${BASE_URL}/api/auth/login" \
     -H "Content-Type: application/json" \
     -d "{\"username\": \"$USERNAME\", \"password\": \"$PASSWORD\"}")

# Check if login was successful
TOKEN=$(echo $LOGIN_RESPONSE | grep -oP '"token":"\K[^"]+')

if [ -z "$TOKEN" ]; then
    echo -e "${RED}[!] Login failed!${NC}"
    echo "Response: $LOGIN_RESPONSE"
    exit 1
fi

echo -e "${GREEN}[+] Login successful!${NC}"
echo -e "${BLUE}[*] Token received: ${TOKEN:0:10}...${TOKEN: -10}${NC}"

# 2. Get Projects
echo -e "\n${BLUE}[*] Fetching projects...${NC}"

PROJECTS_RESPONSE=$(curl -s -X GET "${BASE_URL}/api/projects" \
     -H "Authorization: Bearer $TOKEN")

# Pretty print response (requires jq, falls back to raw if not found)
if command -v jq &> /dev/null; then
    echo -e "${GREEN}[+] Projects list:${NC}"
    echo -e "${BLUE}ID\tName${NC}"
    echo -e "${BLUE}---------------------------------------${NC}"
    echo "$PROJECTS_RESPONSE" | jq -r '.projects[] | "\(.ID)\t\(.Name)"'
else
    echo -e "${YELLOW}[!] jq not found, printing raw response:${NC}"
    echo "$PROJECTS_RESPONSE"
fi

# 3. Update Project
echo -e "\n${YELLOW}Update Project ID${NC}"
read -p "Enter Project ID to track: " PROJECT_ID

if [ ! -z "$PROJECT_ID" ]; then
    echo -e "${BLUE}[*] Updating project to $PROJECT_ID...${NC}"
    UPDATE_RESPONSE=$(curl -s -X PUT "${BASE_URL}/api/project" \
         -H "Authorization: Bearer $TOKEN" \
         -H "Content-Type: application/json" \
         -d "{\"project_id\": $PROJECT_ID}")

    if command -v jq &> /dev/null; then
        echo "$UPDATE_RESPONSE" | jq .
    else
        echo "$UPDATE_RESPONSE"
    fi
else
    echo -e "${YELLOW}[!] Skipping project update (no ID provided).${NC}"
fi

echo -e "\n${BLUE}=======================================${NC}"
echo -e "${GREEN}   Test Completed                      ${NC}"
echo -e "${BLUE}=======================================${NC}"
