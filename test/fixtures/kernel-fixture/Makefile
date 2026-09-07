.PHONY: all ci_defconfig defconfig mrproper
all:
	@mkdir -p $(O)/arch/arm64/boot
	@printf 'fixture-kernel-image' > $(O)/arch/arm64/boot/Image
ci_defconfig defconfig:
	@mkdir -p $(O); printf 'CONFIG_FIXTURE=y\n' > $(O)/.config
mrproper:
	@rm -rf $(O)
