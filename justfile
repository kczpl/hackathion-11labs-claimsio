default:
  @just --list

deploy app *args:
  cd infra/ansible && ansible-playbook -i inventories/{{app}}.yml {{app}}-server.yml {{args}}

facts app user *args:
  cd infra/ansible && ansible -i inventories/app.yml app -m ansible.builtin.setup -u {{user}} {{args}}

go *args:
  #!/usr/bin/env bash
  set -euo pipefail

  source .env
  cd api
  echo "Building and deploying Go service..."
  # build the api locally first to catch any errors
  GOOS=linux GOARCH=amd64 go build -o claimsio-api cmd/main.go

  # create remote directory if it doesn't exist
  ssh hackathon@hackathon.n8n.claimsio.com "mkdir -p /home/hackathon/api"

  # copy the binary, Dockerfile and run script
  scp -r claimsio-api Dockerfile scripts/run.sh hackathon@hackathon.n8n.claimsio.com:/home/hackathon/api/

  # make run script executable and execute it
  ssh hackathon@hackathon.n8n.claimsio.com "docker stop claimsio-api || true && \
  docker rm claimsio-api || true && \
  chmod +x /home/hackathon/api/run.sh && \
  cd /home/hackathon/api && \
  ./run.sh"

  echo "Deployment completed successfully"