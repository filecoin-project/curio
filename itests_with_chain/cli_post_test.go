package itests

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/test-go/testify/require"
	"golang.org/x/exp/rand"
)

type CliThing struct {
	*exec.Cmd
	*bytes.Buffer
}

func CliEnv() func(name string, args ...string) *CliThing {
	itest := "CURIO_ITEST_DO_NOT_USE=" + strconv.Itoa(rand.Intn(99999))
	return func(name string, args ...string) *CliThing {
		cmd := exec.Command(name, args...)
		cmd.Env = append(cmd.Env, itest)
		cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH"))
		b := &bytes.Buffer{}
		cmd.Stdout = io.MultiWriter(os.Stdout, b)
		cmd.Stderr = io.MultiWriter(os.Stderr, b)
		return &CliThing{cmd, b}
	}
}
func TestCliPost(t *testing.T) {
	thistest := CliEnv()
	require.NoError(t, os.WriteFile("/tmp/base.toml", []byte(""), 0644))
	require.NoError(t, thistest("../curio", "config", "set", "/tmp/base.toml").Run())
	cmd := thistest("../curio", "test", "window-post", "here")
	require.NoError(t, cmd.Run())
	require.Equal(t, "All tasks complete\n", cmd.Buffer.String())
}
