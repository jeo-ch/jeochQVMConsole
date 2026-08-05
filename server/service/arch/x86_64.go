package arch

// ==================== x86_64 Profile ====================

type x8664Profile struct{}

func (p *x8664Profile) Arch() string                    { return ArchX8664 }
func (p *x8664Profile) DisplayName() string             { return "x86_64 (AMD64)" }
func (p *x8664Profile) EmulatorPath() string            { return "/usr/bin/qemu-system-x86_64" }
func (p *x8664Profile) DefaultMachineType() string      { return "q35" }
func (p *x8664Profile) SupportedMachineTypes() []string { return []string{"q35", "pc"} }
func (p *x8664Profile) DefaultBootType() string         { return "bios" }
func (p *x8664Profile) SupportedBootTypes() []string    { return []string{"bios", "uefi", "uefi-secure"} }
func (p *x8664Profile) DefaultCPUMode() string          { return "host-passthrough" }
func (p *x8664Profile) SupportedDiskBus() []string      { return []string{"virtio", "scsi", "sata", "ide"} }
func (p *x8664Profile) GetCDROMBus() string             { return "sata" }
func (p *x8664Profile) SupportedNicModels() []string    { return []string{"virtio", "e1000e", "rtl8139"} }
func (p *x8664Profile) SupportsBIOS() bool              { return true }
func (p *x8664Profile) SupportsSecureBoot() bool        { return true }
func (p *x8664Profile) SupportsPAE() bool               { return true }
func (p *x8664Profile) SupportsAPIC() bool              { return true }
func (p *x8664Profile) DefaultWatchdogModel() string    { return "itco" }

func (p *x8664Profile) DefaultCPUModel(virtType string) string {
	if virtType == "qemu" {
		return "qemu64"
	}
	return ""
}

func (p *x8664Profile) UEFIFirmwarePath(secureBoot bool) string {
	if secureBoot {
		candidates := []string{
			"/usr/share/edk2/ovmf/OVMF_CODE.secboot.fd",
			"/usr/share/edk2/ovmf/OVMF_CODE.ms.fd",
			"/usr/share/edk2/ovmf/OVMF_CODE.4m.fd",
			"/usr/share/OVMF/OVMF_CODE_4M.ms.fd",
			"/usr/share/OVMF/OVMF_CODE_4M.secboot.fd",
			"/usr/share/OVMF/OVMF_CODE_4M.sec.fd",
		}
		return pickFirstExistingPath(candidates, "/usr/share/edk2/ovmf/OVMF_CODE.secboot.fd")
	}
	candidates := []string{
		"/usr/share/edk2/ovmf/OVMF_CODE.4m.fd",
		"/usr/share/edk2/ovmf/OVMF_CODE.fd",
		"/usr/share/OVMF/OVMF_CODE_4M.fd",
		"/usr/share/OVMF/OVMF_CODE.fd",
	}
	return pickFirstExistingPath(candidates, "/usr/share/edk2/ovmf/OVMF_CODE.fd")
}

func (p *x8664Profile) UEFIVarsTemplatePath(secureBoot bool) string {
	if secureBoot {
		candidates := []string{
			"/usr/share/edk2/ovmf/OVMF_VARS.secboot.fd",
			"/usr/share/edk2/ovmf/OVMF_VARS.ms.fd",
			"/usr/share/edk2/ovmf/OVMF_VARS.4m.fd",
			"/usr/share/OVMF/OVMF_VARS_4M.ms.fd",
			"/usr/share/OVMF/OVMF_VARS_4M.secboot.fd",
			"/usr/share/OVMF/OVMF_VARS.ms.fd",
		}
		return pickFirstExistingPath(candidates, "/usr/share/edk2/ovmf/OVMF_VARS.secboot.fd")
	}
	candidates := []string{
		"/usr/share/edk2/ovmf/OVMF_VARS.4m.fd",
		"/usr/share/edk2/ovmf/OVMF_VARS.fd",
		"/usr/share/OVMF/OVMF_VARS_4M.fd",
		"/usr/share/OVMF/OVMF_VARS.fd",
	}
	return pickFirstExistingPath(candidates, "/usr/share/edk2/ovmf/OVMF_VARS.fd")
}

func init() {
	RegisterProfile(&x8664Profile{})
}
