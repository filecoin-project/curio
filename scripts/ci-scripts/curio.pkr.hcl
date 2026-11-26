packer {
  required_plugins {
    amazon = {
      source  = "github.com/hashicorp/amazon"
      version = "= 1.8.4"
    }
  }
}

variable "build_id" {
  type = string
}

variable "curio_commit" {
  type = string
}

variable "subnet_id" {
  type = string
}

variable "ssh_source_cidr" {
  type = string
}

variable "source_archive" {
  type = string
}

variable "manifest_path" {
  type = string
}

locals {
  ami_name = "filoz-curio-runs-on-${var.build_id}"
}

source "amazon-ebs" "curio" {
  region                                = "us-west-2"
  instance_type                         = "m6i.2xlarge"
  subnet_id                             = var.subnet_id
  associate_public_ip_address           = true
  temporary_security_group_source_cidrs = [var.ssh_source_cidr]
  ami_name                              = local.ami_name
  ami_description                       = "Curio CI image based on RunsOn Ubuntu 24.04 x64"
  ssh_username                          = "ubuntu"
  ssh_timeout                           = "5m"
  ssh_clear_authorized_keys             = true
  ssh_disable_agent_forwarding          = true
  imds_support                          = "v2.0"

  user_data = <<-BOOT
    #!/bin/bash
    systemctl start ssh
  BOOT

  source_ami_filter {
    filters = {
      name                = "runs-on-v2.2-ubuntu24-full-x64-*"
      root-device-type    = "ebs"
      virtualization-type = "hvm"
      architecture        = "x86_64"
    }
    owners      = ["135269210855"]
    most_recent = true
  }

  metadata_options {
    http_endpoint = "enabled"
    http_tokens   = "required"
  }

  aws_polling {
    delay_seconds = 15
    max_attempts  = 240
  }

  launch_block_device_mappings {
    device_name           = "/dev/sda1"
    volume_type           = "gp3"
    volume_size           = 128
    encrypted             = true
    delete_on_termination = true
  }

  run_tags = {
    Name    = local.ami_name
    Project = "CurioCI"
  }

  run_volume_tags = {
    Name    = local.ami_name
    Project = "CurioCI"
  }

  tags = {
    Name        = local.ami_name
    Project     = "CurioCI"
    CurioCommit = var.curio_commit
  }

  snapshot_tags = {
    Name    = local.ami_name
    Project = "CurioCI"
  }
}

build {
  sources = ["source.amazon-ebs.curio"]

  provisioner "file" {
    source      = var.source_archive
    destination = "/tmp/curio-source.tar.gz"
  }

  provisioner "shell" {
    inline = [
      "sudo -n cloud-init status --wait",
      "mkdir /tmp/curio-source",
      "tar -xzf /tmp/curio-source.tar.gz -C /tmp/curio-source",
      "sudo -n bash /tmp/curio-source/scripts/ci-scripts/build-ami.sh",
      "sudo -n systemctl is-active yugabytedb.service postgresql@18-main.service",
      "rm -rf /tmp/curio-source /tmp/curio-source.tar.gz",
      "sudo -n cloud-init clean --logs --machine-id"
    ]
    timeout = "25m"
  }

  post-processor "manifest" {
    output = var.manifest_path
  }
}
