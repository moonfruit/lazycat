#!/usr/bin/env bash
echo --- === Updating packages === ---
go get -u github.com/gorilla/websocket
go mod tidy
echo
