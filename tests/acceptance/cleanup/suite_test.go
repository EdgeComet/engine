package cleanup_test

import (
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCleanup(t *testing.T) {
	RegisterFailHandler(Fail)

	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.ParallelTotal = 1
	suiteConfig.Timeout = 10 * time.Minute
	reporterConfig.Succinct = true

	RunSpecs(t, "Filesystem Cleanup Worker Acceptance Suite", suiteConfig, reporterConfig)
}

var _ = BeforeSuite(func() {
	By("Building edge-gateway binary once for all tests")
	cmd := exec.Command("go", "build", "-o", "../../../bin/edge-gateway", "../../../cmd/edge-gateway")
	cmd.Stdout = GinkgoWriter
	cmd.Stderr = GinkgoWriter
	err := cmd.Run()
	Expect(err).ToNot(HaveOccurred(), "Failed to build edge-gateway")

	By("Verifying binary exists")
	_, err = os.Stat("../../../bin/edge-gateway")
	Expect(err).ToNot(HaveOccurred(), "Binary not found after build")
})
