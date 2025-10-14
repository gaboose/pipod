package podman

type Option struct {
	importOpt
	manifestCreateOpt
}

func (o Option) applyImportOpt(opts *importOpts)                 { o.importOpt(opts) }
func (o Option) applyManifestCreateOpt(opts *manifestCreateOpts) { o.manifestCreateOpt(opts) }
