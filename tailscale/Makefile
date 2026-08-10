all: app.lpk

app.lpk: lzc-* src/*
	lzc-cli project build -o app.lpk

clean:
	rm -f app.lpk content/tsproxy

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall `sed -n 's/^package: *//p' lzc-manifest.yml`

update:
	@./update.sh
