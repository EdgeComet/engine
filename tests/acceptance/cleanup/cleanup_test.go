package cleanup_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"

	"github.com/edgecomet/engine/tests/acceptance/cleanup/testutil"
)

var _ = Describe("Filesystem Cleanup Worker", func() {
	var (
		eg            *exec.Cmd
		cacheBuilder  *testutil.CacheBuilder
		testCachePath string
		configPath    string
	)

	BeforeEach(func() {
		testCachePath = "/tmp/edgecomet_cleanup_test_" + uuid.New().String()
		cacheBuilder = testutil.NewCacheBuilder(testCachePath)
		configPath = prepareTestConfig(testCachePath)
	})

	AfterEach(func() {
		if eg != nil && eg.Process != nil {
			eg.Process.Kill()
			eg.Wait()
		}

		os.RemoveAll(testCachePath)
		os.RemoveAll(filepath.Dir(configPath))
	})

	Describe("Basic cleanup with single host", func() {
		It("should delete old directories and preserve recent ones", func() {
			// Retention = 20m (stale_ttl) + 5m (safety_margin) = 25m
			oldDir1 := cacheBuilder.CreateTestDirectory(999, 30)
			oldDir2 := cacheBuilder.CreateTestDirectory(999, 28)
			recentDir1 := cacheBuilder.CreateTestDirectory(999, 20)
			recentDir2 := cacheBuilder.CreateTestDirectory(999, 10)
			recentDir3 := cacheBuilder.CreateTestDirectory(999, 5)

			eg = startEG(configPath)

			time.Sleep(10 * time.Second)

			Expect(cacheBuilder.DirectoryExists(oldDir1)).To(BeFalse(), "Directory 30m old should be deleted (retention=25m)")
			Expect(cacheBuilder.DirectoryExists(oldDir2)).To(BeFalse(), "Directory 28m old should be deleted (retention=25m)")
			Expect(cacheBuilder.DirectoryExists(recentDir1)).To(BeTrue(), "Directory 20m old should be kept (retention=25m)")
			Expect(cacheBuilder.DirectoryExists(recentDir2)).To(BeTrue(), "Directory 10m old should be kept (retention=25m)")
			Expect(cacheBuilder.DirectoryExists(recentDir3)).To(BeTrue(), "Directory 5m old should be kept (retention=25m)")
		})
	})

	Describe("Multiple hosts with different retention", func() {
		It("should respect per-host retention policies", func() {
			// Host 888: retention = 5m (stale_ttl) + 5m (safety) = 10m
			host888Old := cacheBuilder.CreateTestDirectory(888, 12)
			host888Recent := cacheBuilder.CreateTestDirectory(888, 8)

			// Host 999: retention = 20m (stale_ttl) + 5m (safety) = 25m
			host999Old := cacheBuilder.CreateTestDirectory(999, 28)
			host999Recent := cacheBuilder.CreateTestDirectory(999, 22)

			eg = startEG(configPath)

			time.Sleep(10 * time.Second)

			Expect(cacheBuilder.DirectoryExists(host888Old)).To(BeFalse(), "Host 888: 12m old should be deleted (retention=10m)")
			Expect(cacheBuilder.DirectoryExists(host888Recent)).To(BeTrue(), "Host 888: 8m old should be kept (retention=10m)")
			Expect(cacheBuilder.DirectoryExists(host999Old)).To(BeFalse(), "Host 999: 28m old should be deleted (retention=25m)")
			Expect(cacheBuilder.DirectoryExists(host999Recent)).To(BeTrue(), "Host 999: 22m old should be kept (retention=25m)")
		})
	})

	Describe("Cleanup worker disabled", func() {
		It("should not delete any directories when cleanup disabled", func() {
			oldDir := cacheBuilder.CreateTestDirectory(999, 30)

			disabledConfigPath := prepareDisabledTestConfig(testCachePath)
			eg = startEG(disabledConfigPath)

			time.Sleep(10 * time.Second)

			Expect(cacheBuilder.DirectoryExists(oldDir)).To(BeTrue(), "Directory should not be deleted when cleanup disabled")
		})
	})

	Describe("Host with delete strategy (no stale cache)", func() {
		It("should cleanup based on safety margin only for delete strategy", func() {
			// Host 777: cache.ttl=5m, strategy=delete (no stale_ttl)
			// Path timestamp = expiration time (already includes cache.ttl)
			// Retention = 0 (no additional) + 5m (safety) = 5m
			host777Old := cacheBuilder.CreateTestDirectory(777, 7)
			host777Recent := cacheBuilder.CreateTestDirectory(777, 3)

			// Host 999: stale_ttl=20m, retention=25m (20m+5m safety)
			host999Old := cacheBuilder.CreateTestDirectory(999, 30)
			host999Recent := cacheBuilder.CreateTestDirectory(999, 20)

			missingStaleTTLConfigPath := prepareMissingStaleTTLTestConfig(testCachePath)
			eg = startEG(missingStaleTTLConfigPath)

			time.Sleep(10 * time.Second)

			// Host 777: delete strategy uses only safety_margin
			Expect(cacheBuilder.DirectoryExists(host777Old)).To(BeFalse(), "Host 777: 7m old should be deleted (retention=5m)")
			Expect(cacheBuilder.DirectoryExists(host777Recent)).To(BeTrue(), "Host 777: 3m old should be kept (retention=5m)")

			// Host 999: serve_stale strategy (unchanged)
			Expect(cacheBuilder.DirectoryExists(host999Old)).To(BeFalse(), "Host 999: 30m old should be deleted (retention=25m)")
			Expect(cacheBuilder.DirectoryExists(host999Recent)).To(BeTrue(), "Host 999: 20m old should be kept (retention=25m)")
		})
	})

	Describe("URL pattern stale_ttl override", func() {
		It("should use maximum stale_ttl from patterns for cleanup retention", func() {
			// Host 999: host-level stale_ttl=10m, pattern override stale_ttl=30m
			// Should use max (30m) + 5m safety = 35m retention
			veryOldDir := cacheBuilder.CreateTestDirectory(999, 40)
			oldDir := cacheBuilder.CreateTestDirectory(999, 25)
			recentDir := cacheBuilder.CreateTestDirectory(999, 12)

			patternOverrideConfigPath := preparePatternOverrideTestConfig(testCachePath)
			eg = startEG(patternOverrideConfigPath)

			time.Sleep(10 * time.Second)

			// 40m old should be deleted (beyond 35m retention)
			Expect(cacheBuilder.DirectoryExists(veryOldDir)).To(BeFalse(), "40m old should be deleted (retention=35m)")

			// 25m old should be kept (within 35m retention - proves pattern override used)
			Expect(cacheBuilder.DirectoryExists(oldDir)).To(BeTrue(), "25m old should be kept (retention=35m, pattern override)")

			// 12m old should be kept (would be deleted if using host-level 15m retention)
			Expect(cacheBuilder.DirectoryExists(recentDir)).To(BeTrue(), "12m old should be kept (proves not using host-level)")
		})
	})

	Describe("Nested directory structure", func() {
		It("should delete entire directory tree including nested content", func() {
			// Retention = 20m (stale_ttl) + 5m (safety_margin) = 25m
			nestedDir := cacheBuilder.CreateNestedDirectory(999, 30)

			eg = startEG(configPath)

			time.Sleep(10 * time.Second)

			// Entire minute directory should be deleted (30m old, retention=25m)
			Expect(cacheBuilder.DirectoryExists(nestedDir)).To(BeFalse(), "Nested directory tree should be deleted")
		})
	})

	Describe("Empty host directory", func() {
		It("should handle missing host directory without errors", func() {
			// Don't create any cache directories for host 999
			// Host is configured but directory doesn't exist

			eg = startEG(configPath)

			time.Sleep(10 * time.Second)

			// Should complete without errors - verify process still running
			Expect(eg.Process).ToNot(BeNil(), "Process should be running")
			Expect(eg.ProcessState).To(BeNil(), "Process should not have exited")
		})
	})

	Describe("Global cache config fallback", func() {
		It("should use global cache config when host has no cache config", func() {
			// Global config: stale_ttl=10m, safety_margin=5m
			// Expected retention = 10m + 5m = 15m
			// Host 555: no cache config defined (should inherit global)
			host555Old := cacheBuilder.CreateTestDirectory(555, 18)
			host555Recent := cacheBuilder.CreateTestDirectory(555, 12)

			globalConfigPath := prepareGlobalConfigTestConfig(testCachePath)
			eg = startEG(globalConfigPath)

			time.Sleep(10 * time.Second)

			// 18m old should be deleted (beyond 15m retention)
			Expect(cacheBuilder.DirectoryExists(host555Old)).To(BeFalse(), "Host 555: 18m old should be deleted (global retention=15m)")

			// 12m old should be kept (within 15m retention - proves global config used)
			Expect(cacheBuilder.DirectoryExists(host555Recent)).To(BeTrue(), "Host 555: 12m old should be kept (proves global config used)")
		})
	})

	Describe("Config inheritance with host cache but no expired section", func() {
		It("should inherit global expired config when host has cache.ttl but no expired section", func() {
			// Global config: stale_ttl=10m, safety_margin=5m
			// Expected retention = 10m + 5m = 15m
			// Host 666: has cache.ttl but no expired section (should inherit global expired)
			host666Old := cacheBuilder.CreateTestDirectory(666, 18)
			host666Recent := cacheBuilder.CreateTestDirectory(666, 12)

			inheritanceConfigPath := prepareInheritanceTestConfig(testCachePath)
			eg = startEG(inheritanceConfigPath)

			time.Sleep(10 * time.Second)

			// 18m old should be deleted (beyond 15m retention)
			Expect(cacheBuilder.DirectoryExists(host666Old)).To(BeFalse(), "Host 666: 18m old should be deleted (inherited global retention=15m)")

			// 12m old should be kept (within 15m retention - proves inheritance works)
			Expect(cacheBuilder.DirectoryExists(host666Recent)).To(BeTrue(), "Host 666: 12m old should be kept (proves global expired inherited)")
		})
	})

	Describe("Empty parent directory cleanup", func() {
		It("should remove empty parent directories after deleting minute directories", func() {
			// Retention = 20m (stale_ttl) + 5m (safety_margin) = 25m
			// Create two old directories in different year/month/day/hour that will be deleted (over 365 days to ensure different year)
			oldDir1 := cacheBuilder.CreateTestDirectory(999, 530000) // Will be deleted (~368 days ago)
			oldDir2 := cacheBuilder.CreateTestDirectory(999, 530002) // Will be deleted (~368 days 2min ago)

			// Keep references to parent directories to verify they're removed
			hourDir := filepath.Dir(oldDir1)
			dayDir := filepath.Dir(hourDir)
			monthDir := filepath.Dir(dayDir)
			yearDir := filepath.Dir(monthDir)
			hostDir := filepath.Dir(yearDir)

			// Create a recent directory in a different hour (will be kept)
			recentDir := cacheBuilder.CreateTestDirectory(999, 10) // Will be kept
			recentHourDir := filepath.Dir(recentDir)

			eg = startEG(configPath)

			time.Sleep(10 * time.Second)

			// Verify old minute directories are deleted
			Expect(cacheBuilder.DirectoryExists(oldDir1)).To(BeFalse(), "Old minute directory 1 should be deleted")
			Expect(cacheBuilder.DirectoryExists(oldDir2)).To(BeFalse(), "Old minute directory 2 should be deleted")

			// Verify empty parent directories (hour, day, month, year) are removed
			Expect(cacheBuilder.DirectoryExists(hourDir)).To(BeFalse(), "Empty hour directory should be removed")
			Expect(cacheBuilder.DirectoryExists(dayDir)).To(BeFalse(), "Empty day directory should be removed")
			Expect(cacheBuilder.DirectoryExists(monthDir)).To(BeFalse(), "Empty month directory should be removed")
			Expect(cacheBuilder.DirectoryExists(yearDir)).To(BeFalse(), "Empty year directory should be removed")

			// Verify recent directory and its parents are kept
			Expect(cacheBuilder.DirectoryExists(recentDir)).To(BeTrue(), "Recent minute directory should be kept")
			Expect(cacheBuilder.DirectoryExists(recentHourDir)).To(BeTrue(), "Recent hour directory should be kept")

			// Verify host directory is kept (contains recent directories)
			Expect(cacheBuilder.DirectoryExists(hostDir)).To(BeTrue(), "Host directory should be kept (contains recent cache)")
		})

		It("should remove empty host directory when all cache is deleted", func() {
			// Retention = 20m (stale_ttl) + 5m (safety_margin) = 25m
			// Create only old directories that will all be deleted
			oldDir1 := cacheBuilder.CreateTestDirectory(999, 30)
			oldDir2 := cacheBuilder.CreateTestDirectory(999, 28)

			hostDir := filepath.Join(testCachePath, "999")

			eg = startEG(configPath)

			time.Sleep(10 * time.Second)

			// Verify all old directories are deleted
			Expect(cacheBuilder.DirectoryExists(oldDir1)).To(BeFalse(), "Old directory 1 should be deleted")
			Expect(cacheBuilder.DirectoryExists(oldDir2)).To(BeFalse(), "Old directory 2 should be deleted")

			// Verify host directory is removed (completely empty)
			Expect(cacheBuilder.DirectoryExists(hostDir)).To(BeFalse(), "Empty host directory should be removed")
		})

		It("should preserve partially empty directories with other content", func() {
			// Retention = 20m (stale_ttl) + 5m (safety_margin) = 25m
			// Create old directory in one hour and recent directory in same hour
			timestamp := time.Now().UTC().Add(-30 * time.Minute)
			oldDir := filepath.Join(
				testCachePath,
				"999",
				timestamp.Format("2006"),
				timestamp.Format("01"),
				timestamp.Format("02"),
				timestamp.Format("15"),
				timestamp.Format("04"),
			)
			os.MkdirAll(oldDir, 0o755)
			os.WriteFile(filepath.Join(oldDir, "test.html"), []byte("<html>test</html>"), 0o644)

			// Create recent directory in same hour but different minute
			recentTimestamp := time.Now().UTC().Add(-20 * time.Minute)
			recentDir := filepath.Join(
				testCachePath,
				"999",
				recentTimestamp.Format("2006"),
				recentTimestamp.Format("01"),
				recentTimestamp.Format("02"),
				recentTimestamp.Format("15"),
				recentTimestamp.Format("04"),
			)
			os.MkdirAll(recentDir, 0o755)
			os.WriteFile(filepath.Join(recentDir, "test.html"), []byte("<html>test</html>"), 0o644)

			hourDir := filepath.Dir(recentDir)

			eg = startEG(configPath)

			time.Sleep(10 * time.Second)

			// Verify old directory is deleted
			Expect(cacheBuilder.DirectoryExists(oldDir)).To(BeFalse(), "Old minute directory should be deleted")

			// Verify recent directory is kept
			Expect(cacheBuilder.DirectoryExists(recentDir)).To(BeTrue(), "Recent minute directory should be kept")

			// Verify hour directory is kept (contains recent minute directory)
			Expect(cacheBuilder.DirectoryExists(hourDir)).To(BeTrue(), "Hour directory should be kept (contains recent cache)")
		})
	})
})

func startEG(configPath string) *exec.Cmd {
	cmd := exec.Command("../../../bin/edge-gateway", "-c", configPath)

	// Capture output for debugging (only if DEBUG env var is set)
	if os.Getenv("DEBUG") != "" || os.Getenv("VERBOSE") != "" {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}

	err := cmd.Start()
	Expect(err).ToNot(HaveOccurred(), "Edge Gateway should start successfully")

	time.Sleep(2 * time.Second)

	return cmd
}

type TestConfigOptions struct {
	HostsFixture    string
	CleanupDisabled bool
	TempDirPrefix   string
}

func prepareTestConfigWithOptions(cachePath string, opts TestConfigOptions) string {
	fixtureConfig := "fixtures/edge-gateway-test.yaml"

	if opts.HostsFixture == "" {
		opts.HostsFixture = "hosts-cleanup-test.yaml"
	}
	if opts.TempDirPrefix == "" {
		opts.TempDirPrefix = "cleanup-test-config-"
	}

	fixtureHosts := "fixtures/" + opts.HostsFixture

	configData, err := os.ReadFile(fixtureConfig)
	Expect(err).ToNot(HaveOccurred(), "Failed to read fixture config")

	var config map[string]interface{}
	err = yaml.Unmarshal(configData, &config)
	Expect(err).ToNot(HaveOccurred(), "Failed to parse config YAML")

	storage := config["storage"].(map[string]interface{})
	storage["base_path"] = cachePath

	if opts.CleanupDisabled {
		cleanup := storage["cleanup"].(map[string]interface{})
		cleanup["enabled"] = false
	}

	tempDir, err := os.MkdirTemp("", opts.TempDirPrefix+"*")
	Expect(err).ToNot(HaveOccurred(), "Failed to create temp dir for config")

	tempConfigPath := filepath.Join(tempDir, "edge-gateway-test.yaml")
	tempHostsPath := filepath.Join(tempDir, opts.HostsFixture)

	hostsData, err := os.ReadFile(fixtureHosts)
	Expect(err).ToNot(HaveOccurred(), "Failed to read fixture hosts")

	err = os.WriteFile(tempHostsPath, hostsData, 0o644)
	Expect(err).ToNot(HaveOccurred(), "Failed to write temp hosts config")

	hosts := config["hosts"].(map[string]interface{})
	hosts["include"] = tempHostsPath

	updatedConfig, err := yaml.Marshal(config)
	Expect(err).ToNot(HaveOccurred(), "Failed to marshal updated config")

	err = os.WriteFile(tempConfigPath, updatedConfig, 0o644)
	Expect(err).ToNot(HaveOccurred(), "Failed to write temp config")

	return tempConfigPath
}

func prepareTestConfig(cachePath string) string {
	return prepareTestConfigWithOptions(cachePath, TestConfigOptions{
		HostsFixture: "hosts-cleanup-test.yaml",
	})
}

func prepareDisabledTestConfig(cachePath string) string {
	return prepareTestConfigWithOptions(cachePath, TestConfigOptions{
		HostsFixture:    "hosts-cleanup-test.yaml",
		CleanupDisabled: true,
	})
}

func prepareMissingStaleTTLTestConfig(cachePath string) string {
	return prepareTestConfigWithOptions(cachePath, TestConfigOptions{
		HostsFixture: "hosts-cleanup-missing-stale-ttl.yaml",
	})
}

func preparePatternOverrideTestConfig(cachePath string) string {
	return prepareTestConfigWithOptions(cachePath, TestConfigOptions{
		HostsFixture: "hosts-cleanup-pattern-override.yaml",
	})
}

func prepareGlobalConfigTestConfig(cachePath string) string {
	return prepareTestConfigWithOptions(cachePath, TestConfigOptions{
		HostsFixture: "hosts-cleanup-global-config.yaml",
	})
}

func prepareInheritanceTestConfig(cachePath string) string {
	return prepareTestConfigWithOptions(cachePath, TestConfigOptions{
		HostsFixture: "hosts-cleanup-inheritance.yaml",
	})
}
