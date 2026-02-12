APP_TITLE := autorun
VERSION := 0.1.0
APP_NAME := ${APP_TITLE}_v${VERSION}.exe
BUILD_ROUTE := ./bin/${APP_NAME}
SRC_ROUTE := ./src/main.go

install:
	@go mod download

build:
	@go build -o ${BUILD_ROUTE} ${SRC_ROUTE}

tidy:
	@go mod tidy
	@go mod vendor

run: tidy build
	@${BUILD_ROUTE}

dev: build
	@${BUILD_ROUTE}

release: tidy
	@go build -o ./release/${APP_NAME} ${SRC_ROUTE}

build2:
	@go build -${BUILD_ROUTE} -tags embedenv ${SRC_ROUTE}

run2: tidy build2
	@${BUILD_ROUTE}

