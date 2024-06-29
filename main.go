package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const containersDir = "./containers"
const rootFsTarball = "./ubuntu-base-22.04-base-amd64.tar.gz"

func main() {
	if len(os.Args) < 2 {
		log.Fatal("a command is required")
	}

	command := os.Args[1]
	switch command {
	case "run", "_child":
		// the run command will just init a new isolated process i.e the container with _child command,
		// in which we will actually run the command. so we first create a container and then inside
		// it we run the command that user specified

		if len(os.Args) > 2 {
			run(os.Args[2:], command == "_child")
		} else {
			run([]string{}, command == "_child")
		}

	default:
		log.Fatal("bad command")
	}
}

func run(args []string, isChild bool) {
	if len(args) == 0 {
		log.Fatal("at least 1 argument is required")
	}

	// if isChild is true, then it means that we're inside the container

	var commandName string
	var commandArgs []string
	if isChild {
		// if this is the child process, then we run the command that the user passed
		commandName = args[0]
		if len(args) > 1 {
			commandArgs = args[1:]
		}
	} else {
		// otherwise we'll run this program itself in a separate process with an internal
		// _child command and it will be responsible for running user specified command
		path, err := os.Executable()
		abortIfError(err, "os.Executable()")
		commandName = path
		commandArgs = append(commandArgs, "_child")
		commandArgs = append(commandArgs, args...)
	}

	// create Cmd struct to execute the given command
	cmd := exec.Command(commandName, commandArgs...)

	// wire child process's stdin, stdout & stderr to that of current process
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if isChild {
		containerId := "b-" + randomString(16)

		// set hostname inside container to a random string
		abortIfError(syscall.Sethostname([]byte(containerId)), "set hostname")

		// extract the rootfs tarball
		rootfsDir := filepath.Join(containersDir, containerId)
		fmt.Println("", rootfsDir)
		unzipRootFsTarball(rootfsDir, rootFsTarball)

		// set the root directory inside the container (to debian bookworm's rootfs)
		abortIfError(syscall.Chroot(rootfsDir), "chroot")

		// set current directory to the new root directory
		abortIfError(syscall.Chdir("/"), "chdir")

		// set procfs: tell kernel that for this process (& it's children), use this new /proc directory as procfs
		// first arg can be empty (or anything) because for procfs, the kernal ignores it
		abortIfError(syscall.Mount("", "/proc", "proc", 0, ""), "mount procfs")
		defer syscall.Unmount("/proc", 0)

		// if we were to configure the above things in the main process, then it would have
		// modified the system's hostname, root etc.

		fmt.Println("pid", os.Getpid(), "running", commandName)
	} else {
		// we want the child process that we're about to fork to be isolated
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags:
			// UTS namespace: isolates hostname and domain name
			syscall.CLONE_NEWUTS |
				// PID namespace: isolates process IDs
				syscall.CLONE_NEWPID |
				// Mount namespace: isolates mount points
				syscall.CLONE_NEWNS,

			// unshare container's mount points with the host
			// basically, i've created a new mount namespace for my container about
			// & i don't want it to be shared with the host
			Unshareflags: syscall.CLONE_NEWNS,
		}
	}

	abortIfError(cmd.Run(), "cmd.Run()")
}

func abortIfError(err error, label string) {
	if err != nil {
		log.Fatal(label, err)
	}
}

const randomStringChars string = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(length int) string {
	if length < 1 {
		return ""
	}

	availableRunes := []rune(randomStringChars)

	r := make([]rune, length)

	for i := 0; i < length; i++ {
		r[i] = availableRunes[rand.Intn(len(randomStringChars))]
	}

	return string(r)
}

func unzipRootFsTarball(dest string, src string) {
	abortIfError(os.MkdirAll(dest, 0700), "unzipRootFsTarball(): os.MkdirAll()")

	cmd := exec.Command("tar", []string{"-xzf", src, "-C", dest}...)
	abortIfError(cmd.Run(), "unzipRootFsTarball(): tar cmd.Run()")
}
