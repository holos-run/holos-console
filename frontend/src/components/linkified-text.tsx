const URL_REGEX = /(https:\/\/[^\s]+)/g

interface LinkifiedTextProps {
  text: string
}

export function LinkifiedText({ text }: LinkifiedTextProps) {
  if (!text) return <></>

  const parts = text.split(URL_REGEX)

  return (
    <>
      {parts.map((part, i) =>
        URL_REGEX.test(part) ? (
          <a
            key={i}
            href={part}
            target="_blank"
            rel="noopener noreferrer"
            className="underline text-primary"
          >
            {part}
          </a>
        ) : (
          <span key={i}>{part}</span>
        )
      )}
    </>
  )
}
