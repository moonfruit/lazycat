.PHONY: all clean install uninstall update
.DELETE_ON_ERROR:

all: app.lpk

app.lpk: lzc-* package.yml build.sh hass-oidc-auth.lock hass-oidc-auth.LICENSE.md $(shell find src -type f)
	lzc-cli project build -o app.lpk

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall "$$(yq -r .package package.yml)"

clean:
	rm -rf app.lpk dist

update:
	./update.sh
