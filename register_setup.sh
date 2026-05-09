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
echo -e "${BLUE}      PR Reviewer AI - User Registration ${NC}"
echo -e "${BLUE}==========================================${NC}"

# 1. Collect Registration Details
echo -e "${YELLOW}Create New Account${NC}"
read -p "Username: " USERNAME
read -sp "Password: " PASSWORD
echo ""
read -p "GitLab Personal Access Token: " TOKEN
read -p "GitLab Base URL (e.g., https://gitlab.com): " WEB_URL

echo -e "\n${BLUE}[*] Registering user...${NC}"

REGISTER_RESPONSE=$(curl -s -X POST "${BASE_URL}/api/auth/register" \
     -H "Content-Type: application/json" \
     -d "{
           \"username\": \"$USERNAME\",
           \"password\": \"$PASSWORD\",
           \"token\": \"$TOKEN\",
           \"webUrl\": \"$WEB_URL\"
         }")

# Check if registration was successful
if echo "$REGISTER_RESPONSE" | grep -q "user created successfully"; then
    echo -e "${GREEN}[+] Registration successful!${NC}"
    echo -e "${BLUE}You can now use setup.sh to log in.${NC}"
else
    echo -e "${RED}[!] Registration failed!${NC}"
    if command -v jq &> /dev/null; then
        echo "$REGISTER_RESPONSE" | jq .
    else
        echo "Response: $REGISTER_RESPONSE"
    fi
    exit 1
fi

echo -e "\n${BLUE}==========================================${NC}"
echo -e "${GREEN}   Setup Completed                        ${NC}"
echo -e "${BLUE}==========================================${NC}"
