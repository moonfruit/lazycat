all: app.lpk

app.lpk: lzc-*
	lzc-cli project build -o app.lpk

clean:
	rm -f app.lpk

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall `yq .package lzc-manifest.yml`
