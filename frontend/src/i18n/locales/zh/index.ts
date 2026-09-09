import pool from './pool'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import guide from './guide'
import admin from './admin'
import misc from './misc'
import lottery from './lottery'

export default {
  ...pool,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  ...guide,
  admin,
  ...misc,
  ...lottery,
}
