package predictors

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	ctrl "sigs.k8s.io/controller-runtime"

	konsumeratorv1alpha1 "github.com/lwolf/konsumerator/api/v1alpha1"
	"github.com/lwolf/konsumerator/pkg/providers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var tLogger logr.Logger

func testLogger() logr.Logger {
	if tLogger != nil {
		return tLogger
	}
	return ctrl.Log.WithName("naive_test")
}

func genPromSpec(ratePerCode int64, ramPerCode resource.Quantity) *konsumeratorv1alpha1.PrometheusAutoscalerSpec {
	return &konsumeratorv1alpha1.PrometheusAutoscalerSpec{
		Address:       nil,
		MinSyncPeriod: nil,
		Offset:        konsumeratorv1alpha1.OffsetQuerySpec{},
		Production:    konsumeratorv1alpha1.ProductionQuerySpec{},
		Consumption:   konsumeratorv1alpha1.ConsumptionQuerySpec{},
		RatePerCore:   &ratePerCode,
		RamPerCore:    ramPerCode,
		TolerableLag:  nil,
		CriticalLag:   nil,
		RecoveryTime:  &metav1.Duration{Duration: time.Minute * 30},
	}
}

func TestEstimateCpu(t *testing.T) {
	tests := map[string]struct {
		consumption  int64
		ratePerCore  int64
		expectedCpuR int64
		expectedCpuL int64
	}{
		"simple case": {
			consumption:  20000,
			ratePerCore:  10000,
			expectedCpuR: 2000,
			expectedCpuL: 2000,
		},
		"cpuLimit should be rounded to the CPU": {
			consumption:  18000,
			ratePerCore:  10000,
			expectedCpuR: 1800,
			expectedCpuL: 2000,
		},
		"cpuRequest should be rounded to 100 millicore": {
			consumption:  18510,
			ratePerCore:  10000,
			expectedCpuR: 1900,
			expectedCpuL: 2000,
		},
	}
	for testName, tt := range tests {
		estimator := NaivePredictor{log: testLogger()}
		cpuR, cpuL := estimator.estimateCpu(tt.consumption, tt.ratePerCore)
		if cpuR != tt.expectedCpuR {
			t.Logf("%s: expected Request CPU %d, got %d", testName, tt.expectedCpuR, cpuR)
			t.Fail()
		}
		if cpuL != tt.expectedCpuL {
			t.Logf("%s: expected Limit CPU %d, got %d", testName, tt.expectedCpuL, cpuL)
			t.Fail()
		}
	}
}

func TestEstimateMemory(t *testing.T) {
	tests := map[string]struct {
		consumption     int64
		ramPerCore      int64
		cpuR            int64
		cpuL            int64
		expectedMemoryR int64
		expectedMemoryL int64
	}{
		"simple case": {
			consumption:     20000,
			ramPerCore:      1000,
			cpuR:            2000,
			cpuL:            2000,
			expectedMemoryR: 2000,
			expectedMemoryL: 2000,
		},
	}
	for testName, tt := range tests {
		estimator := NaivePredictor{log: testLogger()}
		memoryR, memoryL := estimator.estimateMemory(tt.consumption, tt.ramPerCore, tt.cpuR, tt.cpuL)
		if memoryR != tt.expectedMemoryR {
			t.Logf("%s: expected Request Memory %d, got %d", testName, tt.expectedMemoryR, memoryR)
			t.Fail()
		}
		if memoryL != tt.expectedMemoryL {
			t.Logf("%s: expected Limit Memory %d, got %d", testName, tt.expectedMemoryL, memoryL)
			t.Fail()
		}
	}

}

func TestExpectedConsumption(t *testing.T) {
	tests := map[string]struct {
		promSpec            konsumeratorv1alpha1.PrometheusAutoscalerSpec
		lagStore            providers.MetricsProvider
		partition           int32
		expectedConsumption int64
	}{
		"expected consumption = production rate without any lag": {
			promSpec:            *genPromSpec(10000, resource.MustParse("1G")),
			lagStore:            NewMockProvider(map[int32]int64{0: 20001}, map[int32]int64{0: 20000}, map[int32]int64{0: 0}),
			partition:           0,
			expectedConsumption: 20001,
		},
		"account for 10 minutes lag": {
			promSpec:            *genPromSpec(10000, resource.MustParse("1G")),
			lagStore:            NewMockProvider(map[int32]int64{0: 20001}, map[int32]int64{0: 20002}, map[int32]int64{0: 10 * 60 * 20000}),
			partition:           0,
			expectedConsumption: 26656,
		},
	}
	for testName, tt := range tests {
		estimator := NaivePredictor{
			lagSource: tt.lagStore,
			promSpec:  &tt.promSpec,
			log:       testLogger(),
		}
		actual := estimator.expectedConsumption(tt.partition)
		if actual != tt.expectedConsumption {
			t.Logf("%s: expected consumption %d, got %d", testName, tt.expectedConsumption, actual)
			t.Fail()
		}
	}
}

func TestEstimateResources(t *testing.T) {
	tests := map[string]struct {
		containerName     string
		promSpec          konsumeratorv1alpha1.PrometheusAutoscalerSpec
		lagStore          providers.MetricsProvider
		partition         int32
		expectedResources corev1.ResourceRequirements
	}{
		"base estimation": {
			containerName: "test",
			promSpec:      *genPromSpec(10000, resource.MustParse("1G")),
			lagStore:      NewMockProvider(map[int32]int64{0: 20000}, map[int32]int64{0: 20001}, map[int32]int64{0: 0}),
			partition:     0,
			expectedResources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2G"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("2G"),
				},
			},
		},
		"low production rate": {
			containerName: "test",
			promSpec:      *genPromSpec(10000, resource.MustParse("1G")),
			lagStore:      NewMockProvider(map[int32]int64{0: 200}, map[int32]int64{0: 20001}, map[int32]int64{0: 0}),
			partition:     0,
			expectedResources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("100M"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("1"),
					corev1.ResourceMemory: resource.MustParse("1G"),
				},
			},
		},
		"allocate no resources if no metrics data provided (dummy provider)": {
			containerName: "test",
			promSpec:      *genPromSpec(10000, resource.MustParse("1G")),
			lagStore:      NewMockProvider(map[int32]int64{0: 0}, map[int32]int64{0: 0}, map[int32]int64{0: 0}),
			partition:     0,
			expectedResources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("0"),
					corev1.ResourceMemory: resource.MustParse("0"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("0"),
					corev1.ResourceMemory: resource.MustParse("0"),
				},
			},
		},
		"no resource allocation if no metrics data provided and no limits are set (dummy provider)": {
			containerName:     "test",
			promSpec:          *genPromSpec(10000, resource.MustParse("1G")),
			lagStore:          NewMockProvider(map[int32]int64{0: 0}, map[int32]int64{0: 0}, map[int32]int64{0: 0}),
			partition:         0,
			expectedResources: corev1.ResourceRequirements{},
		},
	}
	for testName, tt := range tests {
		estimator := NaivePredictor{
			lagSource: tt.lagStore,
			promSpec:  &tt.promSpec,
			log:       testLogger(),
		}
		resources := estimator.Estimate(tt.containerName, tt.partition)
		if resources.Requests.Cpu().MilliValue() != tt.expectedResources.Requests.Cpu().MilliValue() {
			t.Logf("%s: expected request cpu %s, got %s", testName, tt.expectedResources.Requests.Cpu().String(), resources.Requests.Cpu().String())
			t.Fail()
		}
		if resources.Requests.Memory().MilliValue() != tt.expectedResources.Requests.Memory().MilliValue() {
			t.Logf("%s: expected request memory %s, got %s", testName, tt.expectedResources.Requests.Memory().String(), resources.Requests.Memory().String())
			t.Fail()
		}
		if resources.Limits.Cpu().MilliValue() != tt.expectedResources.Limits.Cpu().MilliValue() {
			t.Logf("%s: expected limit cpu %s, got %s", testName, tt.expectedResources.Limits.Cpu().String(), resources.Limits.Cpu().String())
			t.Fail()
		}
		if resources.Limits.Memory().MilliValue() != tt.expectedResources.Limits.Memory().MilliValue() {
			t.Logf("%s: expected limit memory %s, got %s", testName, tt.expectedResources.Limits.Memory().String(), resources.Limits.Memory().String())
			t.Fail()
		}
	}
}

type MockProvider struct {
	messagesBehind  map[int32]int64
	productionRate  map[int32]int64
	consumptionRate map[int32]int64
}

func (m *MockProvider) GetProductionRate(partition int32) int64 {
	production, ok := m.productionRate[partition]
	if !ok {
		return 0
	}
	return production
}
func (m *MockProvider) GetConsumptionRate(partition int32) int64 {
	consumption, ok := m.consumptionRate[partition]
	if !ok {
		return 0
	}
	return consumption
}
func (m *MockProvider) GetMessagesBehind(partition int32) int64 {
	behind, ok := m.messagesBehind[partition]
	if !ok {
		return 0
	}
	return behind
}
func (m *MockProvider) GetLagByPartition(partition int32) time.Duration {
	behind := m.GetMessagesBehind(partition)
	production := m.GetProductionRate(partition)
	if production == 0 {
		return 0
	}
	return time.Duration(behind/production) * time.Second

}
func (m *MockProvider) Update() error { return nil }
func (m *MockProvider) Load(production, consumption, lag map[int32]int64) {
	m.productionRate = production
	m.consumptionRate = consumption
	m.messagesBehind = lag
}

func NewMockProvider(production, consumption, lag map[int32]int64) *MockProvider {
	return &MockProvider{
		messagesBehind:  lag,
		productionRate:  production,
		consumptionRate: consumption,
	}
}
