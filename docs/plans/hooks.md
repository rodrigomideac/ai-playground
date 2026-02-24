I want to have a flexible way to let users customize the vagrant box.

base-iso/packer/default-provision/:
this folder will contain scripts. each script will be executed in order lexicographically. For example:
0.defaul-provision.sh
1.docker.sh

base-iso/packer/custom-provision/:
Scripts here take precedence over the default-provision scripts. For example:

default-provision/:
0.default-provision.sh
1.docker.sh
custom-provision/:
0.my-custom-provision.sh

We expect that custom-provision/0.my-custom-provision.sh followed by defaul-provision/1.docker.sh are going to be installed

need to update claude md, readme.md
create docs/custom-provisionining.md containing detailed documentation.

break dependencies.sh into defaul-provision/ scripts.
update the template.pkr.hcl to meet this specs.
