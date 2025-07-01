all: app.lpk

app.lpk: lzc-* dist/vue/*
	lzc-cli project build -o app.lpk

clean:
	rm -f app.lpk

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall `yq .package lzc-manifest.yml`

update:
	@./update.sh
