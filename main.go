//go:build linux

package main

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const containersDir = "./containers"
const rootFsTarball = "./ubuntu-base-22.04-base-amd64.tar.gz"

func init() {
	exitIfError(os.MkdirAll(containersDir, 0700), "init containersDir")
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("a command is required")
	}

	command := os.Args[1]
	switch command {
	case "run", "_child":
		// the run command will just init a new isolated process (i.e the container) with _child command,
		// in which we will actually run the command. so we first create a container and then inside
		// it we run the command that user specified

		var volumes []string
		var args []string

		if len(os.Args) > 2 {
			for _, arg := range os.Args[2:] {
				if strings.HasPrefix(arg, "-v=") {
					volumes = append(volumes, strings.TrimPrefix(arg, "-v="))
				} else {
					args = append(args, arg)
				}
			}
		}

		run(args, volumes, command == "_child")

	case "ps":
		ps()

	default:
		log.Fatal("bad command")
	}
}

func run(args []string, volumes []string, isChild bool) {
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
		exitIfError(err, "os.Executable()")
		commandName = path
		commandArgs = append(commandArgs, "_child")

		// pass the volumes again with -v= add command-line arguments
		if len(volumes) > 0 {
			volumeArgs := make([]string, len(volumes))
			for i, vol := range volumes {
				volumeArgs[i] = "-v=" + vol
			}

			commandArgs = append(commandArgs, volumeArgs...)
		}

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
		exitIfError(syscall.Sethostname([]byte(containerId)), "set hostname")

		// extract the rootfs tarball
		rootfsDir := filepath.Join(containersDir, containerId)
		unzipRootFsTarball(rootfsDir, rootFsTarball)

		// map volumes to share storage between host & container
		mountedVolumes := make([]string, len(volumes))
		for i, volume := range volumes {
			parts := strings.Split(volume, ":")
			if len(parts) != 2 {
				log.Fatalf("invalid volume mapping: %s", volume)
			}

			source := parts[0]
			target := filepath.Join(rootfsDir, parts[1])

			exitIfError(os.MkdirAll(target, 0700), "mkdir target")
			exitIfError(syscall.Mount(source, target, "", syscall.MS_BIND|syscall.MS_REC, ""), "mount volume")

			// add to the list of mounted volumes
			mountedVolumes[i] = parts[1]
		}

		// defer the unmounting of all volumes
		defer func() {
			for _, target := range mountedVolumes {
				if err := syscall.Unmount(target, 0); err != nil {
					log.Printf("failed to unmount %s: %v", target, err)
				}
			}
		}()

		// set the root directory inside the container to the extracted rootfs
		// abortIfError(syscall.Chroot(rootfsDir), "chroot")
		pivotRoot(rootfsDir)

		// set procfs: tell kernel that for this process (& it's children), use this new /proc directory as procfs
		// for procfs, first arg can be anything ig because the kernal ignores it (based on chat with claude & my experiments)
		exitIfError(syscall.Mount("proc", "/proc", "proc", 0, ""), "mount procfs")
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

	err := cmd.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	os.Exit(cmd.ProcessState.ExitCode())
}

func ps() {
	files, err := os.ReadDir(containersDir)
	exitIfError(err, "ps(): os.ReadDir()")

	for _, file := range files {
		fileInfo, err := file.Info()
		exitIfError(err, "ps(): file.Info()")

		fmt.Println(file.Name(), fileInfo.ModTime().Format(time.UnixDate))
	}
}

func exitIfError(err error, label string) {
	if err != nil {
		if len(label) > 0 {
			log.Fatal(label, ": ", err)
		} else {
			log.Fatal(err)
		}
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
	exitIfError(os.MkdirAll(dest, 0700), "unzipRootFsTarball(): os.MkdirAll()")

	cmd := exec.Command("tar", []string{"-xzf", src, "-C", dest}...)
	exitIfError(cmd.Run(), "unzipRootFsTarball(): tar cmd.Run()")
}

func pivotRoot(newRoot string) {
	// pivot_root system call requires new_root arg to be a mount point. here's a line from man pages
	// new_root must be a path to a mount point, but can't be "/".  A path that is not already a mount point can be converted into one by bind mounting the path onto itself.
	exitIfError(
		syscall.Mount(newRoot, newRoot, "", syscall.MS_BIND|syscall.MS_REC, ""),
		"pivotRoot(): syscall.Mount",
	)

	// put_old must be a subdirectory inside new_root
	putOld := filepath.Join(newRoot, ".put_old")
	exitIfError(os.MkdirAll(putOld, 0700), "pivotRoot(): putold os.MkdirAll")

	// use pivot_root system call to set the root directory inside the container to the extracted rootfs
	exitIfError(syscall.PivotRoot(newRoot, putOld), "pivotRoot(): pivot_root")

	// set current working directory to the new root directory
	exitIfError(syscall.Chdir("/"), "chdir")
}
