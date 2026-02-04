package recache_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RS Capacity Calculation", func() {
	BeforeEach(func() {
		testEnv.ClearRedis()
	})

	Context("Capacity Reservation (Scenario 7)", func() {
		It("should calculate available capacity with 30% reservation for online traffic", func() {
			// Add mock RS with 100 total capacity, 0 current load
			// This means 100 free tabs
			err := testEnv.AddMockRSToRegistry("rs-1", 100, 0)
			Expect(err).ToNot(HaveOccurred())

			// Daemon config has rs_capacity_reserved: 0.30 (30%)
			// Expected calculation:
			// - Total free tabs: 100 - 0 = 100
			// - Reserved for online: 100 * 0.30 = 30
			// - Available for recache: 100 - 30 = 70

			// Get capacity status from daemon
			capacityStatus := testEnv.GetRSCapacityStatus()

			Expect(capacityStatus.TotalFreeTabs).To(Equal(100), "Total free tabs should be 100")
			Expect(capacityStatus.ReservedForOnline).To(Equal(30), "Reserved tabs should be 30 (30% of 100)")
			Expect(capacityStatus.AvailableForRecache).To(Equal(70), "Available for recache should be 70")
			Expect(capacityStatus.ReservationPercent).To(Equal(30.0), "Reservation percent should be 30%")
		})

		It("should calculate correctly with partial RS load", func() {
			// Add mock RS with 100 capacity, 40 load
			// Free tabs = 100 - 40 = 60
			err := testEnv.AddMockRSToRegistry("rs-2", 100, 40)
			Expect(err).ToNot(HaveOccurred())

			// Expected calculation:
			// - Total free tabs: 100 - 40 = 60
			// - Reserved for online: 60 * 0.30 = 18
			// - Available for recache: 60 - 18 = 42

			capacityStatus := testEnv.GetRSCapacityStatus()

			Expect(capacityStatus.TotalFreeTabs).To(Equal(60), "Total free tabs should be 60")
			Expect(capacityStatus.ReservedForOnline).To(Equal(18), "Reserved tabs should be 18 (30% of 60)")
			Expect(capacityStatus.AvailableForRecache).To(Equal(42), "Available for recache should be 42")
		})

		It("should aggregate capacity across multiple RS instances", func() {
			// Add multiple RS instances
			// RS-1: capacity=100, load=20 -> free=80
			// RS-2: capacity=50, load=10 -> free=40
			// RS-3: capacity=30, load=30 -> free=0
			// Total free: 80 + 40 + 0 = 120

			err := testEnv.AddMockRSToRegistry("rs-multi-1", 100, 20)
			Expect(err).ToNot(HaveOccurred())
			err = testEnv.AddMockRSToRegistry("rs-multi-2", 50, 10)
			Expect(err).ToNot(HaveOccurred())
			err = testEnv.AddMockRSToRegistry("rs-multi-3", 30, 30)
			Expect(err).ToNot(HaveOccurred())

			// Expected calculation:
			// - Total free tabs: 80 + 40 + 0 = 120
			// - Reserved for online: 120 * 0.30 = 36
			// - Available for recache: 120 - 36 = 84

			capacityStatus := testEnv.GetRSCapacityStatus()

			Expect(capacityStatus.TotalFreeTabs).To(Equal(120), "Total free tabs should be sum of all RS free tabs")
			Expect(capacityStatus.ReservedForOnline).To(Equal(36), "Reserved should be 30% of total free")
			Expect(capacityStatus.AvailableForRecache).To(Equal(84), "Available should be 120 - 36")
		})

		It("should return zero when no RS instances available", func() {
			// Clear Redis to ensure no RS instances
			testEnv.ClearRedis()

			capacityStatus := testEnv.GetRSCapacityStatus()

			Expect(capacityStatus.TotalFreeTabs).To(Equal(0))
			Expect(capacityStatus.ReservedForOnline).To(Equal(0))
			Expect(capacityStatus.AvailableForRecache).To(Equal(0))
			Expect(capacityStatus.ReservationPercent).To(Equal(30.0), "Reservation percent config should still be 30%")
		})

		It("should return zero when all RS instances are at full capacity", func() {
			// Add RS at full load
			err := testEnv.AddMockRSToRegistry("rs-full", 50, 50)
			Expect(err).ToNot(HaveOccurred())

			capacityStatus := testEnv.GetRSCapacityStatus()

			Expect(capacityStatus.TotalFreeTabs).To(Equal(0))
			Expect(capacityStatus.ReservedForOnline).To(Equal(0))
			Expect(capacityStatus.AvailableForRecache).To(Equal(0))
		})
	})
})
