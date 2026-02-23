package acceptance_test

import (
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// createConfigTestCommand creates a command for config validation testing
// It sets the working directory to the project root to ensure go.mod is accessible
func createConfigTestCommand(args ...string) *exec.Cmd {
	// Build full command: go run ./cmd/edge-gateway [args...]
	cmdArgs := append([]string{"run", "./cmd/edge-gateway"}, args...)
	cmd := exec.Command("go", cmdArgs...)

	// Set working directory to project root (three levels up from tests/acceptance/basic)
	cmd.Dir = "../../.."

	return cmd
}

var _ = Describe("Config Validation", func() {
	Context("when running validation only", func() {
		It("should succeed with valid configuration", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("configuration test is successful"))
			Expect(string(output)).To(ContainSubstring("syntax is ok"))
		})

		It("should fail with invalid configuration", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/invalid_config.yaml", "-t")
			output, err := cmd.CombinedOutput()

			Expect(err).To(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Configuration validation FAILED"))
			Expect(string(output)).To(ContainSubstring("invalid server.listen"))
		})

		It("should return exit code 0 for valid config", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t")
			err := cmd.Run()

			Expect(err).NotTo(HaveOccurred())
		})

		It("should return exit code 1 for invalid config", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/invalid_config.yaml", "-t")
			err := cmd.Run()

			Expect(err).To(HaveOccurred())
			exitErr, ok := err.(*exec.ExitError)
			Expect(ok).To(BeTrue())
			Expect(exitErr.ExitCode()).To(Equal(1))
		})
	})

	Context("when testing URLs", func() {
		It("should show render action for blog URL", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/blog/post")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("=== Host: example.com"))
			Expect(string(output)).To(ContainSubstring("Action: render"))
			Expect(string(output)).To(ContainSubstring("Cache TTL"))
			Expect(string(output)).To(ContainSubstring("Rendering:"))
		})

		It("should show status action for admin URL", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/admin/users")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Action: status_403"))
			Expect(string(output)).To(ContainSubstring("Response: 403 Forbidden"))
		})

		It("should show bypass action with cache for static files", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/static/main.js")
			output, err := cmd.CombinedOutput()
			// fmt.Println("========================")
			// fmt.Println(string(output))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Action: bypass"))
			Expect(string(output)).To(ContainSubstring("Bypass Cache: enabled"))
			Expect(string(output)).To(ContainSubstring("Cached Status Codes"))
		})

		It("should test relative URL across all hosts", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "/blog/post")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Testing URL: /blog/post"))
			Expect(string(output)).To(ContainSubstring("Checking across"))
			Expect(string(output)).To(ContainSubstring("=== Host: example.com"))
			Expect(string(output)).To(ContainSubstring("=== Host: shop.example.com"))
			Expect(string(output)).To(ContainSubstring("=== Host: blog.example.com"))
			Expect(string(output)).To(ContainSubstring("=== Host: some-eshop.com"))
		})

		It("should show error for unknown host", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://unknown-domain.com/page")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("ERROR: Host \"unknown-domain.com\" not found"))
			Expect(string(output)).To(ContainSubstring("Available hosts:"))
			Expect(string(output)).To(ContainSubstring("example.com"))
		})

		It("should show normalized URL and hash", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/blog/post")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Normalized URL:"))
			Expect(string(output)).To(ContainSubstring("URL Hash:"))
		})

		It("should show host_id in output", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/blog/post")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("=== Host: example.com (host_id: 1) ==="))
		})

		It("should normalize URLs by lowercasing host", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://EXAMPLE.COM/blog/post")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			outputStr := string(output)
			Expect(outputStr).To(ContainSubstring("URL: https://EXAMPLE.COM/blog/post"))
			Expect(outputStr).To(ContainSubstring("Normalized URL: https://example.com/blog/post"))
		})

		It("should normalize URLs by sorting query parameters", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/page?z=3&a=1&m=2")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			outputStr := string(output)
			Expect(outputStr).To(ContainSubstring("URL: https://example.com/page?z=3&a=1&m=2"))
			Expect(outputStr).To(ContainSubstring("Normalized URL: https://example.com/page?a=1&m=2&z=3"))
		})

		It("should show matched pattern", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/blog/article-123")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Matched Pattern: /blog/*"))
		})

		It("should show default pattern for unmatched URLs", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://shop.example.com/unmatched/path")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			outputStr := string(output)

			// Find the shop.example.com section
			if strings.Contains(outputStr, "=== Host: shop.example.com") {
				Expect(outputStr).To(ContainSubstring("Matched Pattern: (default)"))
			}
		})
	})

	Context("when testing pattern matching", func() {
		It("should match wildcard patterns", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/blog/2024/jan/post")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Matched Pattern: /blog/*"))
			Expect(string(output)).To(ContainSubstring("Action: render"))
		})

		It("should match exact patterns", func() {
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Matched Pattern: /"))
		})

		It("should show first match wins", func() {
			// Admin pattern should match before any default
			cmd := createConfigTestCommand("-c", "tests/integration/fixtures/configtest-url-tester/edge-gateway.yaml", "-t", "https://example.com/admin/dashboard")
			output, err := cmd.CombinedOutput()

			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("Matched Pattern: /admin/*"))
			Expect(string(output)).To(ContainSubstring("Action: status_403"))
		})
	})
})
