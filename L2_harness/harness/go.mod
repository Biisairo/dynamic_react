module github.com/Biisairo/dynamic_react/L2_harness/harness

go 1.23.0

require (
	github.com/Biisairo/dynamic_react/L0_core/contract v0.0.0
	github.com/Biisairo/dynamic_react/L0_core/core v0.0.0
	github.com/Biisairo/dynamic_react/L1_inject/inject v0.0.0
	github.com/Biisairo/dynamic_react/L1_inject/sets v0.0.0
)

require golang.org/x/text v0.28.0

replace github.com/Biisairo/dynamic_react/L0_core/contract => ../../L0_core/contract

replace github.com/Biisairo/dynamic_react/L0_core/core => ../../L0_core/core

replace github.com/Biisairo/dynamic_react/L1_inject/inject => ../../L1_inject/inject

replace github.com/Biisairo/dynamic_react/L1_inject/sets => ../../L1_inject/sets
