package acceptance_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Storage Configuration Validation", func() {
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "storage-validation-test")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Context("with valid storage configuration", func() {
		It("should validate config with existing directory", func() {
			cacheDir := filepath.Join(tempDir, "cache")
			err := os.MkdirAll(cacheDir, 0o755)
			Expect(err).NotTo(HaveOccurred())

			configPath := createTestConfig(tempDir, cacheDir)
			cmd := createConfigTestCommand("-c", configPath, "-t")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred(), "Output: %s", string(output))
			Expect(string(output)).To(ContainSubstring("configuration test is successful"))
		})

		It("should validate config with non-existent directory that will be auto-created", func() {
			cacheDir := filepath.Join(tempDir, "new-cache")

			_, err := os.Stat(cacheDir)
			Expect(os.IsNotExist(err)).To(BeTrue(), "Directory should not exist yet")

			configPath := createTestConfig(tempDir, cacheDir)
			cmd := createConfigTestCommand("-c", configPath, "-t")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred(), "Output: %s", string(output))
			Expect(string(output)).To(ContainSubstring("configuration test is successful"))

			_, err = os.Stat(cacheDir)
			Expect(err).NotTo(HaveOccurred(), "Directory should have been created during validation")
		})

		It("should validate config with relative path", func() {
			configPath := filepath.Join(tempDir, "edge-gateway.yaml")
			hostsDir := filepath.Join(tempDir, "hosts.d")

			err := os.MkdirAll(hostsDir, 0o755)
			Expect(err).NotTo(HaveOccurred())

			configContent := `internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"
server:
  listen: ":10070"
  timeout: 120s
redis:
  addr: "localhost:6379"
storage:
  base_path: "./cache"
hosts:
  include: "hosts.d/"
`
			err = os.WriteFile(configPath, []byte(configContent), 0o644)
			Expect(err).NotTo(HaveOccurred())

			hostConfigContent := `hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`
			err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostConfigContent), 0o644)
			Expect(err).NotTo(HaveOccurred())

			cmd := createConfigTestCommand("-c", configPath, "-t")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred(), "Output: %s", string(output))
			Expect(string(output)).To(ContainSubstring("configuration test is successful"))
		})
	})

	Context("with invalid storage configuration", func() {
		It("should fail validation with empty base_path", func() {
			configPath := createTestConfig(tempDir, "")
			cmd := createConfigTestCommand("-c", configPath, "-t")
			output, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Configuration validation FAILED"))
			Expect(string(output)).To(ContainSubstring("storage.base_path is required"))
		})

		It("should fail validation with file path instead of directory", func() {
			filePath := filepath.Join(tempDir, "not-a-directory")
			err := os.WriteFile(filePath, []byte("test"), 0o644)
			Expect(err).NotTo(HaveOccurred())

			configPath := createTestConfig(tempDir, filePath)
			cmd := createConfigTestCommand("-c", configPath, "-t")
			output, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Configuration validation FAILED"))
			Expect(string(output)).To(ContainSubstring("not a directory"))
		})
	})
})

func createTestConfig(baseDir, storagePath string) string {
	configPath := filepath.Join(baseDir, "edge-gateway.yaml")
	hostsDir := filepath.Join(baseDir, "hosts.d")

	err := os.MkdirAll(hostsDir, 0o755)
	Expect(err).NotTo(HaveOccurred())

	configContent := `internal:
  listen: "0.0.0.0:10071"
  auth_key: "test-auth-key-12345"
server:
  listen: ":10070"
  timeout: 120s
redis:
  addr: "localhost:6379"
storage:
  base_path: "` + storagePath + `"
hosts:
  include: "hosts.d/"
`
	err = os.WriteFile(configPath, []byte(configContent), 0o644)
	Expect(err).NotTo(HaveOccurred())

	hostConfigContent := `hosts:
  - id: 1
    domain: "example.com"
    render_key: "test-key"
    render:
      timeout: 30s
      dimensions:
        desktop:
          id: 1
          width: 1920
          height: 1080
          render_ua: "Mozilla/5.0"
`
	err = os.WriteFile(filepath.Join(hostsDir, "01-test.yaml"), []byte(hostConfigContent), 0o644)
	Expect(err).NotTo(HaveOccurred())

	return configPath
}
