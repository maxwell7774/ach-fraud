.PHONY: build vet test fmt sqlc web migrate-up migrate-reset e2e seed clean

build: web
	go build -o bin/ach .

vet:
	go vet ./...

test:
	go test ./...

fmt:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed on:"; gofmt -l .; exit 1; }

sqlc:
	sqlc generate

web:
	cd web && bun install && bun run build

migrate-up:
	. ./.env && goose -dir internal/migrate/schema up

migrate-reset:
	. ./.env && psql "$${GOOSE_DBSTRING}" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" && goose -dir internal/migrate/schema up

e2e:
	./hack/e2e.sh

seed:
	./hack/seed.sh

clean:
	rm -f bin/ach
