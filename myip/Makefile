all: app.lpk

.PHONY: all update clean install uninstall
update:
	./update.sh

app.lpk: lzc-* package.yml
	lzc-cli project build -o app.lpk

clean:
	rm -f app.lpk

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall "$$(yq -er '.package' package.yml)"
