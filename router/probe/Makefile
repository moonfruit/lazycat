all: probe.lpk

probe.lpk: lzc-* ../src/probe/main.go
	lzc-cli project build -o probe.lpk

install: probe.lpk
	lzc-cli app install probe.lpk

uninstall:
	lzc-cli app uninstall com.github.moonfruit.router-probe

clean:
	rm -rf probe.lpk dist
