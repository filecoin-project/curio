// Run from curio root.
// requires packages: dpkg-dev
// Usage:
// ~/GitHub/curio$ go run apt/make_debs.go ~/apt-private.b64
// ~/GitHub/curio$ go run apt/make_debs.go ~/apt-private.b64
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/codeskyblue/go-sh"

	"github.com/filecoin-project/curio/build"

	"github.com/filecoin-project/lotus/lib/must"
)

var version string

func main() {
	// TODO!!! This should REQUIRE th path to the APT repo so it can copy
	// the files to the right place and run (depend on) that script,
	// THen include directions to check-in the APT repo
	if len(os.Args) < 2 || strings.EqualFold(os.Args[1], "help") {
		fmt.Println("Usage: make_debs path_to_Base64_enc_private_key.asc")
		fmt.Println("Run this from the root of the curio repo as it runs 'make'.")
		os.Exit(1)
	}

	version = build.BuildVersion
	if i := strings.Index(build.BuildVersion, "-"); i != -1 {
		version = build.BuildVersion[:i]
	}
	if i := strings.Index(build.BuildVersion, "_"); i != -1 {
		version = build.BuildVersion[:i]
	}

	AssertPackageInstalled("debsigs")

	// Import the key (repeat imports are OK)
	f := must.One(os.Open(os.Args[1]))
	out := must.One(os.OpenFile("/tmp/private.key", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600))
	_ = must.One(io.Copy(out, base64.NewDecoder(base64.StdEncoding, f)))
	_ = out.Close()
	_ = f.Close()
	OrPanic(sh.Command("gpg", "--import", "/tmp/private.key").Run())
	OrPanic(os.Remove("/tmp/private.key"))

	base, err := os.MkdirTemp(os.TempDir(), "curio-apt")
	OrPanic(err)

	OrPanic(sh.NewSession().Command("make", "deps").Run())
	part2(base, "curio-cuda", "FFI_USE_CUDA_SUPRASEAL=1")
	part2(base, "curio-opencl", "FFI_USE_OPENCL=1")
	fmt.Println("Done. DEB files are in ", base)
}

func AssertPackageInstalled(pkg string) {
	ck := bytes.NewBuffer(nil)
	sess := sh.NewSession()
	sess.Stdout = ck
	OrPanic(sess.Command("apt", "list", pkg).Run())
	if !strings.Contains(ck.String(), "[installed]") {
		fmt.Println(pkg + ` is not installed. Please install it.`)
		os.Exit(1)
	}
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

	// Presume the CPU is newer than '19 and enable SHA
	sess.SetEnv("RUSTFLAGS", "-C target-cpu=native -g")
	sess.SetEnv("FFI_BUILD_FROM_SOURCE", "1")
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
		base = path.Join(dir, "lib", "systemd", "system")
		OrPanic(os.MkdirAll(base, 0755))
		OrPanic(copyFile("apt/curio.service", path.Join(base, "curio.service")))
	}
	// fix the debian/control "package" and "version" fields
	fb, err := os.ReadFile(path.Join(dir, "DEBIAN", "control"))
	OrPanic(err)
	f := string(fb)
	f = strings.ReplaceAll(f, "$PACKAGE", product)
	f = strings.ReplaceAll(f, "$VERSION", version)
	f = strings.ReplaceAll(f, "$ARCH", runtime.GOARCH)

	OrPanic(os.WriteFile(path.Join(dir, "DEBIAN", "control"), []byte(f), 0644))
	fullname := product + "-" + version + "_" + runtime.GOARCH + ".deb"

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
		"debsigs", "--sign=origin", "-k", "B751F6AC4FA6D98F", fullname).Run())
}

func copyFile(src, dest string) error {
	return sh.Command("cp", src, dest).Run()
}

func OrPanic(err error) {
	if err != nil {
		panic(err)
	}
}
