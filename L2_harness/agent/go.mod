module github.com/Biisairo/dynamic_react/L2_harness/agent

go 1.23.0

require (
	github.com/Biisairo/dynamic_react/L0_core/contract v0.0.0
	github.com/Biisairo/dynamic_react/L0_core/core v0.0.0
	github.com/Biisairo/dynamic_react/L1_inject/adapters v0.0.0
	github.com/Biisairo/dynamic_react/L1_inject/sets v0.0.0
	github.com/Biisairo/dynamic_react/L2_harness/harness v0.0.0
	github.com/Biisairo/dynamic_react/L2_harness/osenv v0.0.0
)

require (
	github.com/Biisairo/dynamic_react/L1_inject/inject v0.0.0 // indirect
	golang.org/x/text v0.28.0 // indirect
)

replace github.com/Biisairo/dynamic_react/L0_core/contract => ../../L0_core/contract

replace github.com/Biisairo/dynamic_react/L0_core/core => ../../L0_core/core

replace github.com/Biisairo/dynamic_react/L1_inject/adapters => ../../L1_inject/adapters

replace github.com/Biisairo/dynamic_react/L1_inject/inject => ../../L1_inject/inject

replace github.com/Biisairo/dynamic_react/L1_inject/sets => ../../L1_inject/sets

replace github.com/Biisairo/dynamic_react/L2_harness/harness => ../harness

replace github.com/Biisairo/dynamic_react/L2_harness/osenv => ../osenv
