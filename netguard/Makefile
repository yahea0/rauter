.PHONY: all web agent run test clean

# البناء الكامل: لوحة التحكم أولاً (لتضمينها في الملف التنفيذي) ثم الوكيل
all: web agent

web:
    cd web && npm ci || npm install
    cd web && npm run build
    rm -rf agent/internal/web/dist
    mkdir -p agent/internal/web/dist
    cp -r web/dist/* agent/internal/web/dist/

agent:
    cd agent && go build -ldflags "-s -w" -o ../bin/netguard ./cmd/netguard

run: agent
    ./bin/netguard -config config.json

test:
    cd agent && go vet ./... && go test ./...
    cd ai && python -m compileall .

clean:
    rm -rf bin web/dist web/node_modules
    git checkout -- agent/internal/web/dist 2>/dev/null || (mkdir -p agent/internal/web/dist && echo "<!-- run: make web -->" > agent/internal/web/dist/index.html)
