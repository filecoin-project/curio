// Run from curio root.
// requires packages: dpkg-dev
// Usage:
// ~/GitHub/curio$ go run apt/make_debs.go 0.9.7 ~/apt-private.asc
// ~/GitHub/curio$ go run apt/make_debs.go 0.9.7 ~/apt-private.asc
package main

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/codeskyblue/go-sh"
)

var version string

func main() {
	if len(os.Args) < 3 || strings.EqualFold(os.Args[1], "help") {
		fmt.Println("Usage: make_debs <version> path_to_Base64_enc_private_key.asc")
		fmt.Println("Run this from the root of the curio repo as it runs 'make'.")
		os.Exit(1)
	}

	version = os.Args[1]

	// Import the key (repeat imports are OK)
	OrPanic(sh.Command("base64", "-d", os.Args[2], ">/tmp/private.key").Run())
	OrPanic(sh.Command("gpg", "--import", "/tmp/private.key").Run())
	OrPanic(os.Remove("/tmp/private.key"))

	base, err := os.MkdirTemp(os.TempDir(), "curio-apt")
	OrPanic(err)

	sh.NewSession().SetDir(base).Command("make", "deps").Run()
	part2(base, "curio-cuda", "")
	part2(base, "curio-opencl", "FFI_USE_OPENCL=1")
	fmt.Println("Done. DEB files are in ", base)
}

func part2(base, product, extra string) {
	// copy apt/debian  to dir/debian
	dir := path.Join(base, product)
	err := os.MkdirAll(path.Join(dir, "DEBIAN"), 0755)
	OrPanic(err)

	OrPanic(sh.Command("cp", "-r", "apt/DEBIAN", dir).Run())
	sess := sh.NewSession()
	for _, env := range strings.Split(extra, " ") {
		if len(env) == 0 {
			continue
		}
		v := strings.Split(env, "=")
		sess.SetEnv(v[0], v[1])
	}
	fmt.Println("making")

	// This ENV is only for fixing this script. It will result in a bad build.
	if os.Getenv("CURIO_DEB_NOBUILD") != "1" {
		// FUTURE: Use cross-compilation to cover more arch and run anywhere.
		// FUTURE: Use RUST & Go PGO.
		OrPanic(sess.Command("make", "clean", "all").Run())
	}

	// strip binaries
	OrPanic(sh.Command("strip", "curio").Run())
	OrPanic(sh.Command("strip", "sptool").Run())

	fmt.Println("copying")
	{
		base := path.Join(dir, "usr", "local", "bin")
		OrPanic(os.MkdirAll(base, 0755))
		OrPanic(copyFile("curio", path.Join(base, "curio")))
		OrPanic(copyFile("sptool", path.Join(base, "sptool")))
		base = path.Join(dir, "etc", "systemd", "system")
		OrPanic(os.MkdirAll(base, 0755))
		OrPanic(copyFile("apt/curio.service", path.Join(base, "curio.service")))
	}
	// fix the debian/control "package" and "version" fields
	f, err := os.ReadFile(path.Join(dir, "DEBIAN", "control"))
	OrPanic(err)
	f = []byte(strings.ReplaceAll(string(f), "$PACKAGE", product))
	f = []byte(strings.ReplaceAll(string(f), "$VERSION", version))
	OrPanic(os.WriteFile(path.Join(dir, "DEBIAN", "control"), f, 0644))
	fullname := product + "-" + version + "_amd64.deb"

	// Option 1: piece by piece. Maybe could work, but it is complex.
	// Build a .changes file
	//OrPanic(sh.Command("dpkg-genchanges", "-b", "-u.").SetDir(dir).Run())
	// Sign the .changes file
	//OrPanic(sh.Command("debsign", "--sign=origin", "--default", path.Join(dir, "..", "*.changes")).Run())
	// Build the .deb file
	//OrPanic(sh.Command("dpkg-deb", "--build", ".").SetDir(dir).Run())

	// Option 2: The following command should sign the deb file.
	// FAIL B/C wants to build.
	//sh.Command("dpkg-buildpackage", "--build=binary").SetDir(dir).Run()

	// Option 3: Use new helpler commands outside of regular DEB stuff.
	OrPanic(sh.NewSession().SetDir(base).Command("dpkg-deb", "-Z", "xz", "--build", product, fullname).Run())

	// Sign the DEB we built.
	OrPanic(sh.NewSession().SetDir(base).Command(
		"dpkg-sig", "--sign", "builder", "-k", "B751F6AC4FA6D98F", fullname).Run())
}

func copyFile(src, dest string) error {
	return sh.Command("cp", src, dest).Run()
}

func OrPanic(err error) {
	if err != nil {
		panic(err)
	}
}
