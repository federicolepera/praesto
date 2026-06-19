package main

import (
	"flag"
	"log"
	"os"

	praestocsi "github.com/federicolepera/praesto/internal/csi"
)

func main() {
	var (
		driverName = flag.String("driver-name", praestocsi.DefaultDriverName, "CSI driver name")
		endpoint   = flag.String("endpoint", "unix:///csi/csi.sock", "CSI endpoint")
		nodeName   = flag.String("node-name", os.Getenv("NODE_NAME"), "Kubernetes node name")
		cacheRoot  = flag.String("cache-root", "/var/praesto", "Praesto model cache root on the host")
	)
	flag.Parse()

	driver := praestocsi.NewDriver(praestocsi.Config{
		DriverName: *driverName,
		Endpoint:   *endpoint,
		NodeName:   *nodeName,
		CacheRoot:  *cacheRoot,
	})

	if err := driver.Run(); err != nil {
		log.Fatalf("CSI node driver exited: %v", err)
	}
}
