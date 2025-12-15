all: app.lpk

app.lpk: lzc-* ../router/go.* ../router/*.go src/*
	lzc-cli project build -o app.lpk

clean:
	rm -rf app.lpk dist

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall `yq .package lzc-manifest.yml`
