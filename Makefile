# -buildvcs=false: builds don't depend on the state of git (and work outside a checkout).
GOFLAGS := -buildvcs=false
VERSION := $(shell cat VERSION)
LDFLAGS := -X dotatrainer/internal/buildinfo.Version=$(VERSION)

.PHONY: all linux windows test lint release install winres cert installer app clean linux-dist linux-install linux-uninstall

all: linux windows

linux:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/dotatrainer .

# -H=windowsgui: the app lives in the tray, so no console window.
windows:
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -ldflags "-H=windowsgui $(LDFLAGS)" -o bin/dotatrainer.exe .

# Regenerates rsrc_windows_amd64.syso (icon, version info, manifest) from winres/ and VERSION.
winres:
	go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64 --product-version $(VERSION).0 --file-version $(VERSION).0

# Creates the self-signed "Dota Trainer" code-signing certificate and trusts it for this Windows user (once).
cert:
	cd /mnt/c && powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$$(wslpath -w $(CURDIR)/installer/new-cert.ps1)"

# Builds the signed installer into dist/ and your Downloads folder.
installer: winres all
	installer/build.sh

# Builds the installer and installs it silently over the current install, keeping settings and statistics.
app: installer
	cd /mnt/c && "$(CURDIR)/dist/DotaTrainer-Setup-$(VERSION).exe" /VERYSILENT /SUPPRESSMSGBOXES /NORESTART

# Formatting, vet for Linux and Windows, then the tests with the race detector.
test:
	@files=$$(gofmt -l $$(git ls-files '*.go')); if [ -n "$$files" ]; then echo "not gofmt'ed:" $$files >&2; exit 1; fi
	go vet ./... && GOOS=windows go vet ./... && go test -race ./...

# staticcheck, pinned so results don't change under us.
lint:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...

# Publishes VERSION: tags the current main commit v$(VERSION) and pushes it. GitHub then
# builds the installer and the Linux tarball and attaches them to a release.
release: test
	@git diff --quiet HEAD || { echo "commit your changes first" >&2; exit 1; }
	@[ "$$(git branch --show-current)" = main ] || { echo "release from main" >&2; exit 1; }
	@! git rev-parse -q --verify refs/tags/v$(VERSION) >/dev/null || { echo "v$(VERSION) is already released; bump VERSION" >&2; exit 1; }
	git tag -a v$(VERSION) -m "Dota Trainer $(VERSION)"
	git push origin main v$(VERSION)

install: all
	mkdir -p $(HOME)/.local/bin
	cp bin/dotatrainer bin/dotatrainer.exe $(HOME)/.local/bin/

# A release for Linux (Arch and friends): the program, a menu entry, the icon and install.sh.
linux-dist:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -trimpath -ldflags "-s -w $(LDFLAGS)" -o dist/linux/dotatrainer .
	cp packaging/linux/dotatrainer.desktop packaging/linux/install.sh dist/linux/
	cp winres/icon.png dist/linux/dotatrainer.png
	tar -C dist -czf dist/dotatrainer-$(VERSION)-linux-x86_64.tar.gz --transform 's,^linux,dotatrainer-$(VERSION),' linux
	@echo "linux release: dist/dotatrainer-$(VERSION)-linux-x86_64.tar.gz"

# Installs into your home folder on this Linux machine (not for WSL, where Windows runs the app).
linux-install: linux-dist
	dist/linux/install.sh

linux-uninstall:
	packaging/linux/install.sh --remove

clean:
	rm -rf bin dist
