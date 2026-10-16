terraform {
  required_version = ">= 1.9.0, < 2.0.0"

  backend "local" {
    path = "state/terraform.tfstate"
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "ap-southeast-1"

  default_tags {
    tags = {
      Application = "device-ingestion"
      Environment = "sandbox"
      ManagedBy   = "terraform"
    }
  }
}
