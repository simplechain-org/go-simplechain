Pod::Spec.new do |spec|
  spec.name         = 'Sipe'
  spec.version      = '{{.Version}}'
  spec.license      = { :type => 'GNU Lesser General Public License, Version 3.0' }
  spec.homepage     = 'https://github.com/simplechain-org/go-simplechain'
  spec.authors      = { {{range .Contributors}}
		'{{.Name}}' => '{{.Email}}',{{end}}
	}
  spec.summary      = 'iOS Simplechain Client'
  spec.source       = { :git => 'https://github.com/simplechain-org/go-simplechain.git', :commit => '{{.Commit}}' }

	spec.platform = :ios
  spec.ios.deployment_target  = '9.0'
	spec.ios.vendored_frameworks = 'Frameworks/Sipe.framework'

	spec.prepare_command = <<-CMD
    curl https://sipestore.blob.core.windows.net/builds/{{.Archive}}.tar.gz | tar -xvz
    mkdir Frameworks
    mv {{.Archive}}/Sipe.framework Frameworks
    rm -rf {{.Archive}}
  CMD
end
