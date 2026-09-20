# -buildvcs=false: builds don't depend on the state of git (and work outside a checkout).
GOFLAGS := -buildvcs=false
VERSION := $(shell cat VERSION)
LDFLAGS := -X gourdian/internal/buildinfo.Version=$(VERSION)

.PHONY: all linux windows test version-check lint notices release install winres cert cert-github installer app clean linux-dist linux-install linux-uninstall

all: linux windows

linux:
	go build $(GOFLAGS) -trimpath -ldflags "$(LDFLAGS)" -o bin/gourdian .

# -H=windowsgui: the app lives in the tray, so no console window.
windows:
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -trimpath -ldflags "-H=windowsgui $(LDFLAGS)" -o bin/gourdian.exe .

# Regenerates rsrc_windows_amd64.syso (icon, version info, manifest) from winres/ and VERSION.
winres:
	go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64 --product-version $(VERSION).0 --file-version $(VERSION).0

# Creates the self-signed "Gourdian" code-signing certificate and trusts it for this Windows user (once).
cert:
	cd /mnt/c && powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$$(wslpath -w $(CURDIR)/installer/new-cert.ps1)"

# Lets the Release workflow sign with the same certificate (once, and after a new make cert).
cert-github:
	installer/cert-to-github.sh

# Builds the signed installer into dist/ and your Downloads folder.
installer: winres all
	installer/build.sh

# Builds the installer and installs it silently over the current install, keeping settings and statistics.
app: installer
	cd /mnt/c && "$(CURDIR)/dist/Gourdian-Setup-$(VERSION).exe" /VERYSILENT /SUPPRESSMSGBOXES /NORESTART

# The version lives in VERSION; PKGBUILD repeats it for makepkg, so they have to agree.
version-check:
	@v=$$(tr -d '[:space:]' < VERSION); p=$$(sed -n 's/^pkgver=//p' packaging/arch/PKGBUILD); \
	[ "$$v" = "$$p" ] || { echo "VERSION is $$v but PKGBUILD says $$p" >&2; exit 1; }

# Formatting, vet for Linux and Windows, then the tests with the race detector.
test: version-check
	@files=$$(gofmt -l $$(git ls-files '*.go')); if [ -n "$$files" ]; then echo "not gofmt'ed:" $$files >&2; exit 1; fi
	go vet ./... && GOOS=windows go vet ./... && go test -race ./...

# The licenses of everything compiled into the app: its libraries, Go itself and the dashboard
# font. Releases ship the file; CI fails when a dependency change leaves it stale.
notices:
	go run github.com/google/go-licenses/v2@v2.0.1 report ./... --ignore gourdian --template packaging/notices.tpl > THIRD_PARTY_NOTICES.txt
	{ printf '\n%s\nGo (runtime and standard library) (BSD-3-Clause)\n\n' "$$(printf '=%.0s' $$(seq 80))"; cat "$$(go env GOROOT)/LICENSE"; \
	  printf '\n%s\nRusso One font (OFL-1.1), in the dashboard\n\n' "$$(printf '=%.0s' $$(seq 80))"; cat internal/server/web/fonts/OFL.txt; } >> THIRD_PARTY_NOTICES.txt

# staticcheck, pinned so results don't change under us.
lint:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...

# Publishes VERSION: tags the current main commit v$(VERSION) and pushes it. The tag is what
# decides the version built and published, so tagging in the GitHub UI works the same way.
# GitHub then
# builds the installer and the Linux tarball and attaches them to a release.
release: test
	@git diff --quiet HEAD || { echo "commit your changes first" >&2; exit 1; }
	@[ "$$(git branch --show-current)" = main ] || { echo "release from main" >&2; exit 1; }
	@! git rev-parse -q --verify refs/tags/v$(VERSION) >/dev/null || { echo "v$(VERSION) is already released; bump VERSION" >&2; exit 1; }
	git tag -a v$(VERSION) -m "Gourdian $(VERSION)"
	git push origin main v$(VERSION)

install: all
	mkdir -p $(HOME)/.local/bin
	cp bin/gourdian bin/gourdian.exe $(HOME)/.local/bin/

# A release for Linux (Arch and friends): the program, a menu entry, the icon and install.sh.
linux-dist:
	rm -rf dist/linux && mkdir -p dist/linux
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -trimpath -ldflags "-s -w $(LDFLAGS)" -o dist/linux/gourdian .
	cp packaging/linux/gourdian.desktop packaging/linux/install.sh LICENSE THIRD_PARTY_NOTICES.txt dist/linux/
	cp winres/icon.png dist/linux/gourdian.png
	tar -C dist -czf dist/gourdian-$(VERSION)-linux-x86_64.tar.gz --transform 's,^linux,gourdian-$(VERSION),' linux
	@echo "linux release: dist/gourdian-$(VERSION)-linux-x86_64.tar.gz"

# Installs into your home folder on this Linux machine (not for WSL, where Windows runs the app).
linux-install: linux-dist
	dist/linux/install.sh

linux-uninstall:
	packaging/linux/install.sh --remove

clean:
	rm -rf bin dist
