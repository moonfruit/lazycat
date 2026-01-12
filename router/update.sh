#!/usr/bin/env bash
echo --- === Updating packages === ---
go -C src get -u gitee.com/linakesi/remotesocks
go -C src mod tidy
echo
