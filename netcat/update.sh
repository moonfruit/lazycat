#!/usr/bin/env bash
echo --- === Updating packages === ---
go -C src get -u github.com/gorilla/websocket
go -C src mod tidy
echo
