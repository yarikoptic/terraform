
resource "aws_s3_bucket" {
  default = {
    arn: "aws:vpc:1a9e0001"
  }

  overrides = {
    // matches aws_s3_bucket.test
    "test": {
      arn: "aws:vpc:1a9e0002"
    }
    "count": {
      0: "aws:vpc:1a9e0003"
    }
    "count": [
      // matches aws_s3_bucket.count.0
      {
        arn: "aws:vpc:1a9e0004"
      },
    ]
    "foreach": {
      // matches aws_s3_bucket.foreach.key
      key: {
        arn: "aws:vpc:1a9e0005"
      }
    }
  }
}

data "aws_vpc" {
  values = [
    {
      arn: "aws:vpc:1a9e4020"
    },
    {
      arn: "aws:vpc:1a9e4019"
    }
  ]
}