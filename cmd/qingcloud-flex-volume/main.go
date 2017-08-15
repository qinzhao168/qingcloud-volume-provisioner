package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"github.com/yunify/qingcloud-volume-provisioner/pkg/volume/qingcloud"
	"github.com/yunify/qingcloud-volume-provisioner/pkg/volume/flex"
)

// fatalf is a convenient method that outputs error in flex volume plugin style
// and quits
func fatalf(msg string, args ...interface{}) {
	err := flex.VolumeResult{
		Message: fmt.Sprintf(msg, args...),
		Status:  "Failure",
	}
	fmt.Printf(err.ToJson())
	os.Exit(1)
}

// printResult is a convenient method for printing result of volume operation
func printResult(result flex.VolumeResult) {
	fmt.Printf(result.ToJson())
	if result.Status == "Success" {
		os.Exit(0)
	}
	os.Exit(1)
}

// ensureVolumeOptions decodes json or die
func ensureVolumeOptions(v string) (vo flex.VolumeOptions) {
	err := json.Unmarshal([]byte(v), &vo)
	if err != nil {
		fatalf("Invalid json options: %s", v)
	}
	return
}

func main() {
	// Used in downloader. To test if the binary is complete
	test := flag.Bool("test", false, "Dry run. To test if the binary is complete")

	// Prepare logs
	os.MkdirAll("/var/log/qingcloud-flex-volume", 0750)
	//log.SetOutput(os.Stderr)

	flag.Set("logtostderr", "true")
	flag.Set("alsologtostderr", "false")
	flag.Set("log_dir", "/var/log/qingcloud-flex-volume")
	flag.Set("stderrThreshold", "fatal")
	flag.Parse()

	if *test {
		return
	}

	volumePlugin, err := qingcloud.NewFlexVolumePlugin()

	if err != nil {
		fatalf("Error init FlexVolumePlugin")
	}


	args := flag.Args()
	if len(args) == 0 {
		fatalf("Usage: %s init|attach|detach|mountdevice|unmountdevice|waitforattach|getvolumename|isattached", os.Args[0])
	}

	var ret flex.VolumeResult
	op := args[0]
	args = args[1:]
	switch op {
	case "init":
		ret = volumePlugin.Init()
	case "attach":
		if len(args) < 2 {
			fatalf("attach requires options in json format and a node name")
		}
		ret = volumePlugin.Attach(ensureVolumeOptions(args[0]), args[1])
	case "isattached":
		if len(args) < 2 {
			fatalf("isattached requires options in json format and a node name")
		}
		ret = volumePlugin.Attach(ensureVolumeOptions(args[0]), args[1])
	case "detach":
		if len(args) < 2 {
			fatalf("detach requires a device path and a node name")
		}
		ret = volumePlugin.Detach(args[0], args[1])
	case "mountdevice":
		if len(args) < 3 {
			fatalf("mountdevice requires a mount path, a device path and mount options")
		}
		ret = volumePlugin.MountDevice(args[0], args[1], ensureVolumeOptions(args[2]))
	case "unmountdevice":
		if len(args) < 1 {
			fatalf("unmountdevice requires a mount path")
		}
		ret = volumePlugin.UnmountDevice(args[0])
	case "waitforattach":
		if len(args) < 2 {
			fatalf("waitforattach requires a device path and options in json format")
		}
		ret = volumePlugin.WaitForAttach(args[0], ensureVolumeOptions(args[1]))
	case "getvolumename":
		if len(args) < 1 {
			fatalf("getvolumename requires options in json format")
		}
		ret = volumePlugin.GetVolumeName(ensureVolumeOptions(args[0]))
	default:
		ret = flex.NewVolumeNotSupported(op)
	}

	printResult(ret)
}