//go:build skiff && !(linux || darwin || freebsd || openbsd)

package pdpnode

func listMountPoints() ([]string, error) {
	return []string{"/"}, nil
}
