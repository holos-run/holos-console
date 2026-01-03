import { useEffect, useState } from 'react'
import { Card, CardContent, Typography, Stack, Box } from '@mui/material'
import { create } from '@bufbuild/protobuf'
import { GetVersionRequestSchema } from '../gen/holos/console/v1/version_pb.js'
import { versionClient } from '../client'

type VersionState = {
  loading: boolean
  error: string | null
  version: {
    version: string
    gitCommit: string
    gitTreeState: string
    buildDate: string
  } | null
}

function formatValue(value: string) {
  return value && value.length > 0 ? value : 'unknown'
}

export function VersionCard() {
  const [state, setState] = useState<VersionState>({
    loading: true,
    error: null,
    version: null,
  })

  useEffect(() => {
    let active = true

    async function loadVersion() {
      try {
        const response = await versionClient.getVersion(
          create(GetVersionRequestSchema),
        )

        if (!active) {
          return
        }

        setState({
          loading: false,
          error: null,
          version: {
            version: response.version,
            gitCommit: response.gitCommit,
            gitTreeState: response.gitTreeState,
            buildDate: response.buildDate,
          },
        })
      } catch (error) {
        if (!active) {
          return
        }

        const message = error instanceof Error ? error.message : 'Unknown error'
        setState({
          loading: false,
          error: message,
          version: null,
        })
      }
    }

    loadVersion()

    return () => {
      active = false
    }
  }, [])

  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="h6" gutterBottom>
          Server Version
        </Typography>
        {state.loading ? (
          <Typography variant="body2">Loading version info...</Typography>
        ) : state.error ? (
          <Typography variant="body2" color="error">
            Failed to load version info: {state.error}
          </Typography>
        ) : (
          <Stack spacing={1.5}>
            <Box>
              <Typography variant="overline" display="block">
                Version
              </Typography>
              <Typography variant="body1">
                {formatValue(state.version?.version ?? '')}
              </Typography>
            </Box>
            <Box>
              <Typography variant="overline" display="block">
                Git Commit
              </Typography>
              <Typography variant="body1">
                {formatValue(state.version?.gitCommit ?? '')}
              </Typography>
            </Box>
            <Box>
              <Typography variant="overline" display="block">
                Git Tree State
              </Typography>
              <Typography variant="body1">
                {formatValue(state.version?.gitTreeState ?? '')}
              </Typography>
            </Box>
            <Box>
              <Typography variant="overline" display="block">
                Build Date
              </Typography>
              <Typography variant="body1">
                {formatValue(state.version?.buildDate ?? '')}
              </Typography>
            </Box>
          </Stack>
        )}
      </CardContent>
    </Card>
  )
}
