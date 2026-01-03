import { Card, CardContent, Typography, Stack, Box } from '@mui/material'
import { useVersion } from '../queries/version'

function formatValue(value: string) {
  return value && value.length > 0 ? value : 'unknown'
}

export function VersionCard() {
  const { data, isLoading, error } = useVersion()
  const errorMessage = error ? error.message : null

  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="h6" gutterBottom>
          Server Version
        </Typography>
        {isLoading ? (
          <Typography variant="body2">Loading version info...</Typography>
        ) : errorMessage ? (
          <Typography variant="body2" color="error">
            Failed to load version info: {errorMessage}
          </Typography>
        ) : (
          <Stack spacing={1.5}>
            <Box>
              <Typography variant="overline" display="block">
                Version
              </Typography>
              <Typography variant="body1">
                {formatValue(data?.version ?? '')}
              </Typography>
            </Box>
            <Box>
              <Typography variant="overline" display="block">
                Git Commit
              </Typography>
              <Typography variant="body1">
                {formatValue(data?.gitCommit ?? '')}
              </Typography>
            </Box>
            <Box>
              <Typography variant="overline" display="block">
                Git Tree State
              </Typography>
              <Typography variant="body1">
                {formatValue(data?.gitTreeState ?? '')}
              </Typography>
            </Box>
            <Box>
              <Typography variant="overline" display="block">
                Build Date
              </Typography>
              <Typography variant="body1">
                {formatValue(data?.buildDate ?? '')}
              </Typography>
            </Box>
          </Stack>
        )}
      </CardContent>
    </Card>
  )
}
