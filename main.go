package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/fatih/color"
	"github.com/pkg/errors"
)

var (
	rgxKindCmd = regexp.MustCompile(`(?i)(?:get|describe|delete|edit)`)
)

type Resource struct {
	ns   string
	name string
}

func (r Resource) String() string {
	return fmt.Sprintf("%s:%s", r.ns, r.name)
}

var helpText = `
kctl is a tool to help make Kubernetes (kubectl) easier to use.

Usage:
  kctl [args]

kctl with no arguments will show this help message. Using --help will show
kubectl help. Generally kctl is aliased as "k" and the rest of the documentation
will reflect this. k proxies all arguments through to kubectl with a few
exceptions where it attempts to augment the arguments passed in to provide
an easier user interface.

There are 2 augmentations that k has over regular kubectl:
1. pattern matching
   When trying to find a service/pod, you can use a colon to denote a "search".
   On the left hand side of the colon is a namespace pattern and on the right
   is a resource pattern. The pattern syntax is described here:
   https://github.com/google/re2/wiki/Syntax

   The best way to understand the use of this is to see some examples
   k get pods :             # Get pods in --all-namespaces
   k get pods default:      # Get pods in a namespace matching "default"
   k describe pod f:^api$   # Describe the pod matching the patterns
   k exec 'f:^api.*' ps afx # Run ps afx on the pod matched by "f:^api$"
   k get services 'def.*:'  # Get all services in a namespace matching "default"

2. ssh command
   The SSH command allows you to easily start a bash session inside a pod
   and carries over your environment's terminal settings so that width/height
   and terminal type are set correctly for the new bash session.

   Example: k ssh default:podname`

func main() {
	if len(os.Args) == 1 {
		fmt.Println(helpText)
		return
	}

	bin, err := exec.LookPath("kubectl")
	if err != nil {
		fmt.Fprintln(os.Stderr, color.RedString("failed to find kubectl binary in path"))
	}

	// For fancy printing
	buildCacheFn := func(res string) ([]Resource, error) {
		fmt.Fprint(os.Stderr, color.BlueString("fetching %s => ", res))
		rs, err := buildCache(res)
		if err != nil {
			// Ensure the error goes on the next line when reported
			fmt.Fprintln(os.Stderr)
			return nil, err
		}

		fmt.Fprintln(os.Stderr, color.BlueString("%d", len(rs)))
		return rs, nil
	}

	args, err := buildArgs(buildCacheFn, os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, color.RedString("%v", err))
		return
	}

	fmt.Fprintln(os.Stderr, color.BlueString("kubectl %s", strings.Join(args, " ")))
	// Using deprecated syscall package because lack of execve in newer sys package
	fmt.Println(syscall.Exec(bin, append([]string{"kubectl"}, args...), os.Environ()))
}

type buildCacheFunction func(string) ([]Resource, error)

func buildArgs(buildCacheFn buildCacheFunction, osArgs []string) ([]string, error) {
	var err error
	var args []string
	var tailArgs []string
	var cache []Resource
	var colonFound bool
	getKind := "pods"

	osArgs = osArgs[1:] // Chop off exe name

	for i, a := range osArgs {
		switch {
		case a == "ssh":
			args = append(args, "exec", "-it")
			tailArgs = append(getSSHArgs(), "/bin/bash")

		case a == ":":
			args = append(args, "--all-namespaces")

		case rgxKindCmd.MatchString(a):
			if i+1 < len(osArgs) {
				args = append(args, a)
				getKind = osArgs[i+1]
			}

		case !colonFound && strings.IndexByte(a, ':') >= 0:
			colonFound = true
			if cache == nil {
				if cache, err = buildCacheFn(getKind); err != nil {
					return nil, err
				}
			}

			splits := strings.Split(a, ":")
			nsPattern, rsPattern := splits[0], splits[1]
			found, err := search(cache, getKind, nsPattern, rsPattern)
			if err != nil {
				return nil, err
			}

			if len(rsPattern) != 0 {
				args = append(args, found.name)
			}
			if len(nsPattern) != 0 || len(rsPattern) != 0 {
				args = append(args, "--namespace", found.ns)
			}

		default:
			args = append(args, a)
		}

	}

	return append(args, tailArgs...), nil
}

func buildCache(res string) (rs []Resource, err error) {
	cmd := exec.Command("kubectl", "get", res, "--no-headers", "--all-namespaces")
	b, err := cmd.Output()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get resource %s", res)
	}

	lines := bytes.Split(b, []byte{'\n'})
	for _, l := range lines {
		if len(l) == 0 {
			continue
		}
		item := bytes.Fields(l)
		rs = append(rs, Resource{ns: string(item[0]), name: string(item[1])})
	}

	return rs, nil
}

func search(rs []Resource, kind string, nsPattern string, rsPattern string) (r Resource, err error) {
	if len(nsPattern) != 0 {
		rgxSpace := regexp.MustCompile(nsPattern)
		rs = keepIf(rs, func(r Resource) bool { return rgxSpace.MatchString(r.ns) })
		if len(rs) == 0 {
			return r, errors.Errorf("no resources found in namespaces matching %s", nsPattern)
		}
	}

	if len(rsPattern) != 0 {
		rgxName := regexp.MustCompile(rsPattern)
		rs = keepIf(rs, func(r Resource) bool { return rgxName.MatchString(r.name) })

		switch len(rs) {
		case 0:
			return r, errors.Errorf("failed to find %s matching %s", kind, rsPattern)
		case 1:
			// continue execution
		default:
			return r, errors.Errorf("ambiguous query for %s matches: %v", kind, rs)
		}
	}

	return rs[0], nil
}

func getSSHArgs() []string {
	sshArgs := []string{"--", "env"}

	term := os.Getenv("TERM")
	if len(term) == 0 {
		term = "xterm"
	}

	sshArgs = append(sshArgs, fmt.Sprintf("TERM=%s", term))

	lines, cols, err := getTermCaps()
	if err == nil {
		sshArgs = append(sshArgs, fmt.Sprintf("COLUMNS=%d", cols), fmt.Sprintf("LINES=%d", lines))
	}

	return sshArgs
}

func getTermCaps() (lines, columns int, err error) {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin // Allows stty to check the term size
	b, err := cmd.Output()
	if err != nil {
		return 0, 0, err
	}

	fragments := bytes.Split(bytes.TrimSpace(b), []byte{' '})
	if len(fragments) == 0 {
		fmt.Fprintln(os.Stderr, color.RedString("failed to get term size:\n%s", b))
	}

	lines, err = strconv.Atoi(string(fragments[0]))
	if err != nil {
		return 0, 0, err
	}

	columns, err = strconv.Atoi(string(fragments[1]))
	if err != nil {
		return 0, 0, err
	}

	return lines, columns, nil
}

func keepIf(rs []Resource, filter func(r Resource) bool) []Resource {
	var keep []Resource
	for _, r := range rs {
		if filter(r) {
			keep = append(keep, r)
		}
	}

	return keep
}
